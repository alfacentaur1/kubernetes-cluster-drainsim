package state

import (
	"slices"
	"testing"

	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/snapshot"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func pod(namespace, name, nodeName string, phase corev1.PodPhase) corev1.Pod {
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name},
		Spec:       corev1.PodSpec{NodeName: nodeName},
		Status:     corev1.PodStatus{Phase: phase},
	}
}

func TestNodesAndBoundPods(t *testing.T) {
	s := New(&snapshot.Snapshot{
		Nodes: []corev1.Node{
			{ObjectMeta: metav1.ObjectMeta{Name: "node-b"}},
			{ObjectMeta: metav1.ObjectMeta{Name: "node-a"}},
		},
		Pods: []corev1.Pod{
			pod("b", "web", "node-a", corev1.PodRunning),
			pod("a", "web", "node-b", corev1.PodRunning),
			pod("a", "api", "node-a", corev1.PodPending),
			pod("a", "pending", "", corev1.PodPending),
			pod("a", "done", "node-a", corev1.PodSucceeded),
			pod("a", "crashed", "node-b", corev1.PodFailed),
		},
	})

	var nodes []string
	for _, n := range s.Nodes() {
		nodes = append(nodes, n.Name)
	}
	if want := []string{"node-a", "node-b"}; !slices.Equal(nodes, want) {
		t.Errorf("Nodes() = %v, want %v", nodes, want)
	}

	var pods []string
	for _, p := range s.BoundPods() {
		pods = append(pods, p.Namespace+"/"+p.Name)
	}
	if want := []string{"a/api", "a/web", "b/web"}; !slices.Equal(pods, want) {
		t.Errorf("BoundPods() = %v, want %v", pods, want)
	}
}

// TestUsageProfiles checks that the profiles are sorted (NFR7) and that
// neither the snapshot nor a caller can change the profiles of the state.
func TestUsageProfiles(t *testing.T) {
	snap := &snapshot.Snapshot{UsageProfiles: []snapshot.UsageProfile{
		{Namespace: "shop", Kind: "Deployment", Name: "web", Containers: []snapshot.ContainerUsage{
			{Name: "sidecar", CPUP95Millicores: 10}, {Name: "app", CPUP95Millicores: 250},
		}},
		{Namespace: "db", Kind: "StatefulSet", Name: "pg"},
		{Namespace: "shop", Kind: "Deployment", Name: "api"},
	}}
	s := New(snap)

	snap.UsageProfiles[0].Containers[0].CPUP95Millicores = 999 // must not reach the state

	got := s.UsageProfiles()
	var names []string
	for _, p := range got {
		names = append(names, p.Namespace+"/"+p.Kind+"/"+p.Name)
	}
	if want := []string{"db/StatefulSet/pg", "shop/Deployment/api", "shop/Deployment/web"}; !slices.Equal(names, want) {
		t.Fatalf("UsageProfiles() = %v, want %v", names, want)
	}
	web := got[2].Containers
	if web[0].Name != "app" || web[1].Name != "sidecar" || web[1].CPUP95Millicores != 10 {
		t.Errorf("containers of web = %+v, want app, sidecar(10m)", web)
	}

	got[2].Containers[0].CPUP95Millicores = 1 // must not reach the state
	if again := s.UsageProfiles()[2].Containers[0].CPUP95Millicores; again != 250 {
		t.Errorf("CPU of web/app after changing a copy = %d, want 250", again)
	}

	if p := New(&snapshot.Snapshot{}).UsageProfiles(); p != nil {
		t.Errorf("UsageProfiles() without profiles = %+v, want nil", p)
	}
}

// TestSetRequests checks that the requests of one container change in every
// pod of the workload, including a pending one, that other containers,
// workloads and limits stay, and that Restore undoes the change (FR23).
func TestSetRequests(t *testing.T) {
	isController := true
	rsPod := func(name, rs, nodeName string) corev1.Pod {
		p := pod("shop", name, nodeName, corev1.PodRunning)
		p.OwnerReferences = []metav1.OwnerReference{{Kind: "ReplicaSet", Name: rs, Controller: &isController}}
		p.Spec.Containers = []corev1.Container{
			{Name: "app", Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:              resource.MustParse("500m"),
					corev1.ResourceEphemeralStorage: resource.MustParse("1Gi"),
				},
				Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1")},
			}},
			{Name: "sidecar", Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m")},
			}},
		}
		return p
	}
	pending := rsPod("web-rs-sim0", "web-rs", "")
	pending.Status.Phase = corev1.PodPending
	s := New(&snapshot.Snapshot{
		ReplicaSets: []appsv1.ReplicaSet{{ObjectMeta: metav1.ObjectMeta{Namespace: "shop", Name: "web-rs",
			OwnerReferences: []metav1.OwnerReference{{Kind: "Deployment", Name: "web", Controller: &isController}}}}},
		Pods: []corev1.Pod{rsPod("web-rs-1", "web-rs", "node-a"), rsPod("web-rs-2", "web-rs", "node-b"), pending,
			rsPod("api-rs-1", "api-rs", "node-a")},
	})
	m := s.Save()

	req := corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("200m"), corev1.ResourceMemory: resource.MustParse("64Mi")}
	if n := s.SetRequests("shop", "Deployment", "web", "app", req); n != 3 {
		t.Fatalf("SetRequests() = %d, want 3 pods", n)
	}
	req[corev1.ResourceCPU] = resource.MustParse("9") // the state keeps its own copy

	cpu := func(p *corev1.Pod, i int) string { return p.Spec.Containers[i].Resources.Requests.Cpu().String() }
	for _, p := range s.WorkloadPods("shop", "Deployment", "web") {
		app := p.Spec.Containers[0].Resources
		if cpu(p, 0) != "200m" || app.Requests.Memory().String() != "64Mi" {
			t.Errorf("%s app requests = %v, want 200m and 64Mi", p.Name, app.Requests)
		}
		if app.Requests.StorageEphemeral().String() != "1Gi" || app.Limits.Cpu().String() != "1" {
			t.Errorf("%s app ephemeral storage or limit changed: %v", p.Name, app)
		}
		if cpu(p, 1) != "100m" {
			t.Errorf("%s sidecar CPU = %s, want 100m", p.Name, cpu(p, 1))
		}
	}
	if api := s.WorkloadPods("shop", "ReplicaSet", "api-rs")[0]; cpu(api, 0) != "500m" {
		t.Errorf("api-rs app CPU = %s, want 500m (another workload)", cpu(api, 0))
	}

	s.Restore(m)
	if web := s.WorkloadPods("shop", "Deployment", "web"); len(web) != 3 || cpu(web[0], 0) != "500m" {
		t.Errorf("after Restore web app CPU = %s, want 500m", cpu(web[0], 0))
	}
	if n := s.SetRequests("shop", "Deployment", "web", "missing", req); n != 0 {
		t.Errorf("SetRequests() for an unknown container = %d, want 0", n)
	}
}
