package policyimpact

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/engine"
	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/engine/psa"
	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/engine/scheduler"
	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/engine/state"
	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/snapshot"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// The pods in these tests have no securityContext. That meets the baseline
// level but violates restricted (runAsNonRoot, allowPrivilegeEscalation,
// capabilities, seccompProfile), so restricted is the "stricter" target.

// A replacement rejected by the target level keeps its budget reduced, so a
// later eviction in the same drain is blocked. The drain passes with the
// current levels and fails only with the target levels (S2, S3, FR31, FR32).
func TestImpactWhenLostReplicaBlocksBudget(t *testing.T) {
	snap := &snapshot.Snapshot{
		Nodes: []corev1.Node{node("node-a"), node("node-b")},
		Pods: []corev1.Pod{
			webPod("web-7d4b9c-aaaaa"),
			webPod("web-7d4b9c-bbbbb"),
		},
		PDBs: []policyv1.PodDisruptionBudget{webPDB(2, 1, 1)}, // one disruption allowed
	}

	res := run(t, snap, map[string]string{"default": "restricted"})

	if res.Impact == nil || res.Impact.Step != 1 || res.Impact.Node != "node-a" {
		t.Fatalf("Impact = %+v, want step 1 at node-a", res.Impact)
	}
	if len(res.Lost) != 1 || res.Lost[0].Pod != "default/web-7d4b9c-sim0" || res.Lost[0].Step != 1 {
		t.Fatalf("Lost = %+v, want the first replacement lost in step 1", res.Lost)
	}
	if !strings.Contains(res.Lost[0].Reason, "restricted") {
		t.Errorf("Lost reason = %q, want the violated level", res.Lost[0].Reason)
	}
	if res.Before.Failed != "" {
		t.Errorf("Before = %+v, want the drain to pass with the current levels", res.Before)
	}
}

// Without a budget the drain still passes, but the application loses a
// replica. Algorithm 5.4 reports no impact; the lost replica is listed (S2).
func TestLostReplicaWithoutImpact(t *testing.T) {
	snap := &snapshot.Snapshot{
		Nodes: []corev1.Node{node("node-a"), node("node-b")},
		Pods:  []corev1.Pod{webPod("web-7d4b9c-aaaaa")},
	}

	res := run(t, snap, map[string]string{"default": "restricted"})

	if res.Impact != nil {
		t.Errorf("Impact = %+v, want none", res.Impact)
	}
	if len(res.Lost) != 1 {
		t.Errorf("Lost = %+v, want one replica", res.Lost)
	}
}

// A target level the pods already meet changes nothing (Algorithm 5.4:
// "report that targets do not affect the drain").
func TestNoImpactWhenPodsMeetTarget(t *testing.T) {
	snap := &snapshot.Snapshot{
		Nodes: []corev1.Node{node("node-a"), node("node-b")},
		Pods:  []corev1.Pod{webPod("web-7d4b9c-aaaaa")},
	}

	res := run(t, snap, map[string]string{"default": "baseline"})

	if res.Impact != nil || len(res.Lost) != 0 || len(res.Running) != 0 {
		t.Errorf("Run() = impact %+v, lost %+v, running %+v, want nothing", res.Impact, res.Lost, res.Running)
	}
}

// A drain that fails already with the current levels is not an impact of
// the target levels: only steps that succeed before and fail after count.
func TestFailureWithCurrentLevelsIsNoImpact(t *testing.T) {
	debug := webPod("debug")
	debug.OwnerReferences = nil // no controller: kubectl drain refuses it (P9)
	snap := &snapshot.Snapshot{
		Nodes: []corev1.Node{node("node-a"), node("node-b")},
		Pods:  []corev1.Pod{debug},
	}

	res := run(t, snap, map[string]string{"default": "restricted"})

	if res.Before.Failed != "node-a" {
		t.Errorf("Before = %+v, want a failure at node-a", res.Before)
	}
	if res.Impact != nil {
		t.Errorf("Impact = %+v, want none", res.Impact)
	}
}

// Running pods that violate the target level are grouped by the workload an
// operator knows: the Deployment, not its ReplicaSet (FR30, S1, Figure 5.6).
func TestRunningViolationsGroupedByDeployment(t *testing.T) {
	snap := &snapshot.Snapshot{
		Nodes: []corev1.Node{node("node-a"), node("node-b")},
		Pods: []corev1.Pod{
			webPod("web-7d4b9c-aaaaa"),
			webPod("web-7d4b9c-bbbbb"),
		},
		ReplicaSets: []appsv1.ReplicaSet{{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:       "default",
				Name:            "web-7d4b9c",
				OwnerReferences: []metav1.OwnerReference{*controller("Deployment", "web")},
			},
		}},
	}

	res := run(t, snap, map[string]string{"default": "restricted"})

	if len(res.Running) != 1 {
		t.Fatalf("Running = %+v, want one workload", res.Running)
	}
	v := res.Running[0]
	if v.Kind != "Deployment" || v.Name != "web" || len(v.Pods) != 2 {
		t.Errorf("Running[0] = %+v, want Deployment web with 2 pods", v)
	}
}

// --- helpers ---

// adapter is shared by all tests, because building the scheduler framework
// is slow. The tests do not run in parallel, so sharing it is safe.
var adapter = sync.OnceValues(func() (*scheduler.Adapter, error) {
	return scheduler.New(context.Background())
})

// run builds the engine over the snapshot and runs the analysis on node-a
// with the given target levels per namespace.
func run(t *testing.T, snap *snapshot.Snapshot, targets map[string]string) Result {
	t.Helper()
	snap.CreatedAt = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	snap.KubernetesVersion = "v1.36.5"

	a, err := adapter()
	if err != nil {
		t.Fatalf("scheduler.New() error = %v", err)
	}
	current, err := psa.New(nil, "")
	if err != nil {
		t.Fatalf("psa.New() error = %v", err)
	}
	target, err := psa.New(targets, "")
	if err != nil {
		t.Fatalf("psa.New(targets) error = %v", err)
	}

	eng := engine.New(state.New(snap), a, current)
	res, err := Run(context.Background(), eng, []string{"node-a"}, current, target)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	return res
}

// node returns a node with room for a few small pods.
func node(name string) corev1.Node {
	alloc := corev1.ResourceList{
		corev1.ResourceCPU:    resource.MustParse("2"),
		corev1.ResourceMemory: resource.MustParse("4Gi"),
		corev1.ResourcePods:   resource.MustParse("110"),
	}
	return corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: name, UID: types.UID("node-" + name),
			Labels: map[string]string{corev1.LabelHostname: name}},
		Status: corev1.NodeStatus{Capacity: alloc, Allocatable: alloc},
	}
}

// webPod returns a Ready pod of the ReplicaSet web-7d4b9c on node-a.
func webPod(name string) corev1.Pod {
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       "default",
			Name:            name,
			GenerateName:    "web-7d4b9c-",
			UID:             types.UID("pod-" + name),
			Labels:          map[string]string{"app": "web"},
			OwnerReferences: []metav1.OwnerReference{*controller("ReplicaSet", "web-7d4b9c")},
		},
		Spec: corev1.PodSpec{
			NodeName: "node-a",
			Containers: []corev1.Container{{
				Name:  "app",
				Image: "registry.k8s.io/pause:3.10",
				Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("100m"),
					corev1.ResourceMemory: resource.MustParse("64Mi"),
				}},
			}},
		},
		Status: corev1.PodStatus{
			Phase:      corev1.PodRunning,
			Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}},
		},
	}
}

// webPDB returns a budget for app=web with the given status.
func webPDB(current, desired, allowed int32) policyv1.PodDisruptionBudget {
	return policyv1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "web-pdb"},
		Spec: policyv1.PodDisruptionBudgetSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "web"}},
		},
		Status: policyv1.PodDisruptionBudgetStatus{
			CurrentHealthy: current, DesiredHealthy: desired,
			DisruptionsAllowed: allowed, ExpectedPods: current,
		},
	}
}

// controller returns a controller reference to a workload.
func controller(kind, name string) *metav1.OwnerReference {
	isController := true
	return &metav1.OwnerReference{APIVersion: "apps/v1", Kind: kind, Name: name,
		UID: types.UID(kind + "-" + name), Controller: &isController}
}
