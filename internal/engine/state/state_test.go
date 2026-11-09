package state

import (
	"slices"
	"testing"

	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/snapshot"
	corev1 "k8s.io/api/core/v1"
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
