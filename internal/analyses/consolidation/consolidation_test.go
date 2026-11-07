package consolidation

import (
	"context"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/engine"
	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/engine/eventlog"
	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/engine/psa"
	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/engine/scheduler"
	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/engine/state"
	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/estimator"
	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/snapshot"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// Three nodes of 2 CPU with 500m, 1200m and 1200m requested. The least
// loaded node-a goes first and its pod fits elsewhere, so it is removed.
// Afterwards a 1200m pod never fits next to the other 1200m pod, so node-b
// and node-c are kept (FR24, FR25, Algorithm 5.3).
func TestExactlyOneNodeRemoved(t *testing.T) {
	snap := threeNodes()

	res := run(t, snap, nil)

	if want := []string{"node-a", "node-b", "node-c"}; !slices.Equal(res.Nodes, want) {
		t.Errorf("Nodes = %v, want %v (sorted by requests)", res.Nodes, want)
	}
	if want := []string{"node-a"}; !slices.Equal(res.Removed, want) {
		t.Fatalf("Removed = %v, want %v", res.Removed, want)
	}
	if len(res.Kept) != 2 || res.Kept[0].Node != "node-b" || res.Kept[1].Node != "node-c" {
		t.Fatalf("Kept = %+v, want node-b and node-c", res.Kept)
	}
	for _, k := range res.Kept {
		if k.Reason == "" {
			t.Errorf("Kept %s has no reason", k.Node)
		}
	}
	if len(res.Events) == 0 {
		t.Fatal("Events is empty, want the events of the drain of node-a")
	}
	for _, ev := range res.Events {
		if ev.Kind == eventlog.Pending || ev.Kind == eventlog.LostReplica {
			t.Errorf("Events contain %+v, want all pods running", ev)
		}
		if ev.Node != "node-a" {
			t.Errorf("Events contain %+v, want only the drain of the removed node-a", ev)
		}
	}
	if len(res.Savings) != 0 {
		t.Errorf("Savings = %+v, want none without a cost model", res.Savings)
	}
}

// A drain passes even when a replacement stays Pending (Section 2.7), but
// such a node is kept: FR24 requires all pods to keep running.
func TestNodeKeptWhenReplacementWouldStayPending(t *testing.T) {
	snap := &snapshot.Snapshot{
		Nodes: []corev1.Node{node("node-a"), node("node-b")},
		Pods: []corev1.Pod{
			pod("node-a", "big-a", "1500m"),
			pod("node-b", "big-b", "1500m"),
		},
	}

	res := run(t, snap, nil)

	if len(res.Removed) != 0 {
		t.Errorf("Removed = %v, want none", res.Removed)
	}
	if len(res.Kept) != 2 {
		t.Fatalf("Kept = %+v, want both nodes", res.Kept)
	}
	for _, k := range res.Kept {
		if !strings.Contains(k.Reason, "would stay pending") {
			t.Errorf("Kept %s reason = %q, want \"would stay pending\"", k.Node, k.Reason)
		}
	}
	if len(res.Events) != 0 {
		t.Errorf("Events = %+v, want none: failed attempts are not reported", res.Events)
	}
}

// The lower bound is ceil(total requests / largest node) for CPU and memory,
// the larger of the two (Section 5.5). Computed by hand: 1500m + 1000m +
// 1000m = 3500m on 2-CPU nodes gives ceil(3500 / 2000) = 2; the memory
// (3 · 64 Mi on 4 Gi nodes) gives 1.
func TestLowerBound(t *testing.T) {
	snap := &snapshot.Snapshot{
		Nodes: []corev1.Node{node("node-a"), node("node-b"), node("node-c")},
		Pods: []corev1.Pod{
			pod("node-a", "a", "1500m"),
			pod("node-b", "b", "1000m"),
			pod("node-c", "c", "1000m"),
		},
	}

	res := run(t, snap, nil)

	if res.LowerBound != 2 {
		t.Errorf("LowerBound = %d, want 2", res.LowerBound)
	}
}

// With a cost model, every removed node gets one saving, estimated while
// the node still exists (FR26).
func TestSavingPerRemovedNode(t *testing.T) {
	cost := &fakeCost{}

	res := run(t, threeNodes(), cost)

	if len(res.Savings) != len(res.Removed) || len(res.Savings) != 1 {
		t.Fatalf("Savings = %+v, want one per removed node %v", res.Savings, res.Removed)
	}
	if res.Savings[0].Node != "node-a" {
		t.Errorf("Savings[0] = %+v, want node-a", res.Savings[0])
	}
	if cost.nilNodes != 0 {
		t.Errorf("cost model got %d nil nodes, want the node before its removal", cost.nilNodes)
	}
}

// --- helpers ---

// fakeCost is a CostModel that records the nodes it is asked about.
type fakeCost struct {
	nilNodes int
}

func (f *fakeCost) Estimate(n *corev1.Node) estimator.Saving {
	if n == nil {
		f.nilNodes++
		return estimator.Saving{}
	}
	return estimator.Saving{Node: n.Name, MoneyKnown: true, Money: 1}
}

// adapter is shared by all tests, because building the scheduler framework
// is slow. The tests do not run in parallel, so sharing it is safe.
var adapter = sync.OnceValues(func() (*scheduler.Adapter, error) {
	return scheduler.New(context.Background())
})

// run builds the engine over the snapshot and runs the consolidation on all
// its nodes.
func run(t *testing.T, snap *snapshot.Snapshot, cost CostModel) Result {
	t.Helper()
	snap.CreatedAt = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	snap.KubernetesVersion = "v1.36.5"

	a, err := adapter()
	if err != nil {
		t.Fatalf("scheduler.New() error = %v", err)
	}
	policy, err := psa.New(nil, "")
	if err != nil {
		t.Fatalf("psa.New() error = %v", err)
	}

	names := make([]string, len(snap.Nodes))
	for i, n := range snap.Nodes {
		names[i] = n.Name
	}
	eng := engine.New(state.New(snap), a, policy)
	res, err := Run(context.Background(), eng, names, cost)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	return res
}

// threeNodes returns the 500m / 1200m / 1200m scenario.
func threeNodes() *snapshot.Snapshot {
	return &snapshot.Snapshot{
		Nodes: []corev1.Node{node("node-a"), node("node-b"), node("node-c")},
		Pods: []corev1.Pod{
			pod("node-a", "small", "500m"),
			pod("node-b", "large-b", "1200m"),
			pod("node-c", "large-c", "1200m"),
		},
	}
}

// node returns a node with 2 CPU and 4 Gi of memory.
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

// pod returns a Ready pod of the ReplicaSet app-<rs> on the node, with the
// given CPU request and 64 Mi of memory.
func pod(nodeName, rs, cpu string) corev1.Pod {
	owner := "app-" + rs
	name := owner + "-aaaaa"
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       "default",
			Name:            name,
			GenerateName:    owner + "-",
			UID:             types.UID("pod-" + name),
			Labels:          map[string]string{"app": rs},
			OwnerReferences: []metav1.OwnerReference{*controller("ReplicaSet", owner)},
		},
		Spec: corev1.PodSpec{
			NodeName: nodeName,
			Containers: []corev1.Container{{
				Name:  "app",
				Image: "registry.k8s.io/pause:3.10",
				Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse(cpu),
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

// controller returns a controller reference to a workload.
func controller(kind, name string) *metav1.OwnerReference {
	isController := true
	return &metav1.OwnerReference{APIVersion: "apps/v1", Kind: kind, Name: name,
		UID: types.UID(kind + "-" + name), Controller: &isController}
}
