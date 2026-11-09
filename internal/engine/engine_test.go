package engine

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/engine/eventlog"
	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/engine/psa"
	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/engine/scheduler"
	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/engine/state"
	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/snapshot"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// A pod of a Deployment is evicted and placed on another node, and a
// DaemonSet pod stays on the drained node (FR6–FR9, FR14).
func TestDrainMovesPodAndSkipsDaemonSet(t *testing.T) {
	web := runningPod("web-7d4b9c-abcde", owner("ReplicaSet", "web-7d4b9c"))
	web.GenerateName = "web-7d4b9c-"
	agent := runningPod("agent-x2k9p", owner("DaemonSet", "agent"))
	agent.Labels = map[string]string{"app": "agent"}

	e, log, st := newEngine(t, &snapshot.Snapshot{
		Nodes: []corev1.Node{node("node-a"), node("node-b")},
		Pods:  []corev1.Pod{web, agent},
	}, nil)

	res, err := e.Drain(context.Background(), []string{"node-a"})
	if err != nil {
		t.Fatalf("Drain() error = %v", err)
	}
	if res.Failed != "" || !slices.Equal(res.Drained, []string{"node-a"}) {
		t.Errorf("Drain() = %+v, want node-a drained", res)
	}
	wantKinds(t, log, eventlog.Cordoned, eventlog.Skipped, eventlog.Evicted, eventlog.Placed, eventlog.NodeDrained)

	if got := podNames(st.PodsOnNode("node-b")); !slices.Equal(got, []string{"web-7d4b9c-sim0"}) {
		t.Errorf("pods on node-b = %v, want [web-7d4b9c-sim0]", got)
	}
	if got := podNames(st.PodsOnNode("node-a")); !slices.Equal(got, []string{"agent-x2k9p"}) {
		t.Errorf("pods on node-a = %v, want [agent-x2k9p]", got)
	}
}

// A pod without a controller is refused, so the drain fails (P9, FR15).
func TestDrainFailsOnPodWithoutController(t *testing.T) {
	e, log, st := newEngine(t, &snapshot.Snapshot{
		Nodes: []corev1.Node{node("node-a"), node("node-b")},
		Pods:  []corev1.Pod{runningPod("debug", nil)},
	}, nil)

	res, err := e.Drain(context.Background(), []string{"node-a"})
	if err != nil {
		t.Fatalf("Drain() error = %v", err)
	}
	if res.Failed != "node-a" || len(res.Drained) != 0 {
		t.Errorf("Drain() = %+v, want node-a failed", res)
	}
	wantKinds(t, log, eventlog.Cordoned, eventlog.Refused, eventlog.DrainFailed)

	if got := podNames(st.PodsOnNode("node-a")); !slices.Equal(got, []string{"debug"}) {
		t.Errorf("pods on node-a = %v, want [debug]", got)
	}
}

// A PodDisruptionBudget that allows no disruption defers the pod, the next
// round brings no progress and the drain fails (P1, FR10).
func TestDrainFailsWhenPDBAllowsNoEviction(t *testing.T) {
	budget := policyv1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "web-pdb"},
		Spec: policyv1.PodDisruptionBudgetSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "web"}},
		},
		Status: policyv1.PodDisruptionBudgetStatus{CurrentHealthy: 1, DesiredHealthy: 1, DisruptionsAllowed: 0, ExpectedPods: 1},
	}
	e, log, st := newEngine(t, &snapshot.Snapshot{
		Nodes: []corev1.Node{node("node-a"), node("node-b")},
		Pods:  []corev1.Pod{runningPod("web-7d4b9c-abcde", owner("ReplicaSet", "web-7d4b9c"))},
		PDBs:  []policyv1.PodDisruptionBudget{budget},
	}, nil)

	res, err := e.Drain(context.Background(), []string{"node-a"})
	if err != nil {
		t.Fatalf("Drain() error = %v", err)
	}
	if res.Failed != "node-a" {
		t.Errorf("Drain() = %+v, want node-a failed", res)
	}
	wantKinds(t, log, eventlog.Cordoned, eventlog.Deferred, eventlog.DrainFailed)

	if got := podNames(st.PodsOnNode("node-a")); !slices.Equal(got, []string{"web-7d4b9c-abcde"}) {
		t.Errorf("pods on node-a = %v, want the pod still there", got)
	}
}

// A placed replacement is assumed to become Ready (Section 5.3), so a later
// drain of its node must respect the budget like for any Ready pod. Found by
// the kind scenario test/scenarios/basic: three replicas with minAvailable 2
// all moved to node-b; draining node-b then evicted all three although their
// replacements stayed Pending, while kubectl drain is blocked after the first.
func TestPlacedReplacementIsProtectedByBudget(t *testing.T) {
	budget := policyv1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "web-pdb"},
		Spec: policyv1.PodDisruptionBudgetSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "web"}},
		},
		Status: policyv1.PodDisruptionBudgetStatus{CurrentHealthy: 3, DesiredHealthy: 2, DisruptionsAllowed: 1, ExpectedPods: 3},
	}
	var pods []corev1.Pod
	for _, name := range []string{"web-7d4b9c-aaaaa", "web-7d4b9c-bbbbb", "web-7d4b9c-ccccc"} {
		p := runningPod(name, owner("ReplicaSet", "web-7d4b9c"))
		p.GenerateName = "web-7d4b9c-"
		pods = append(pods, p)
	}
	e, _, st := newEngine(t, &snapshot.Snapshot{
		Nodes: []corev1.Node{node("node-a"), node("node-b")},
		Pods:  pods,
		PDBs:  []policyv1.PodDisruptionBudget{budget},
	}, nil)

	res, err := e.Drain(context.Background(), []string{"node-a", "node-b"})
	if err != nil {
		t.Fatalf("Drain() error = %v", err)
	}
	if res.Failed != "node-b" || !slices.Equal(res.Drained, []string{"node-a"}) {
		t.Errorf("Drain() = %+v, want node-a drained and node-b failed", res)
	}
	// Only one replica may leave node-b: the budget allows one disruption
	// and its replacement has no node to go to.
	if got := len(st.PodsOnNode("node-b")); got != 2 {
		t.Errorf("pods on node-b = %d, want 2 still running", got)
	}
}

// A replacement that violates the target level is a lost replica: it is not
// placed, but the drain of the node still succeeds (FR31, S2).
func TestDrainLosesReplicaRejectedByTargetLevel(t *testing.T) {
	strict, err := psa.New(map[string]string{"default": "restricted"}, "")
	if err != nil {
		t.Fatalf("psa.New() error = %v", err)
	}
	e, log, st := newEngine(t, &snapshot.Snapshot{
		Nodes: []corev1.Node{node("node-a"), node("node-b")},
		Pods:  []corev1.Pod{runningPod("web-7d4b9c-abcde", owner("ReplicaSet", "web-7d4b9c"))},
	}, strict)

	res, err := e.Drain(context.Background(), []string{"node-a"})
	if err != nil {
		t.Fatalf("Drain() error = %v", err)
	}
	if !slices.Equal(res.Drained, []string{"node-a"}) {
		t.Errorf("Drain() = %+v, want node-a drained", res)
	}
	wantKinds(t, log, eventlog.Cordoned, eventlog.Evicted, eventlog.LostReplica, eventlog.NodeDrained)

	if got := st.PodsOnNode("node-b"); len(got) != 0 {
		t.Errorf("pods on node-b = %v, want none", podNames(got))
	}
	lost := log.Events()[2]
	if !strings.Contains(lost.Reason, "restricted") {
		t.Errorf("LostReplica reason = %q, want the violated level", lost.Reason)
	}
}

// Restoring a checkpoint and draining again gives the same result, including
// the names of the replacements (Memento, NFR7).
func TestRestoreRepeatsTheSameDrain(t *testing.T) {
	web := runningPod("web-7d4b9c-abcde", owner("ReplicaSet", "web-7d4b9c"))
	web.GenerateName = "web-7d4b9c-"
	e, _, st := newEngine(t, &snapshot.Snapshot{
		Nodes: []corev1.Node{node("node-a"), node("node-b")},
		Pods:  []corev1.Pod{web},
	}, nil)

	cp := e.Save()
	if _, err := e.Drain(context.Background(), []string{"node-a"}); err != nil {
		t.Fatalf("first Drain() error = %v", err)
	}
	first := podNames(st.PodsOnNode("node-b"))

	e.Restore(cp)
	if got := podNames(st.PodsOnNode("node-a")); !slices.Equal(got, []string{"web-7d4b9c-abcde"}) {
		t.Fatalf("after Restore, pods on node-a = %v, want the original pod", got)
	}
	if _, err := e.Drain(context.Background(), []string{"node-a"}); err != nil {
		t.Fatalf("second Drain() error = %v", err)
	}
	second := podNames(st.PodsOnNode("node-b"))

	if !slices.Equal(first, second) || !slices.Equal(first, []string{"web-7d4b9c-sim0"}) {
		t.Errorf("pods on node-b: first run %v, second run %v, want [web-7d4b9c-sim0] both times", first, second)
	}
}

// adapter is shared by all tests, because building the scheduler framework
// is slow. The tests do not run in parallel, so sharing it is safe.
var adapter = sync.OnceValues(func() (*scheduler.Adapter, error) {
	return scheduler.New(context.Background())
})

// newEngine builds an engine over the snapshot with a real scheduler adapter.
// A nil policy checks against the levels set on the namespaces.
func newEngine(t *testing.T, snap *snapshot.Snapshot, policy PolicyChecker) (*Engine, *eventlog.Log, *state.ClusterState) {
	t.Helper()
	snap.CreatedAt = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	snap.KubernetesVersion = "v1.36.5"
	st := state.New(snap)

	a, err := adapter()
	if err != nil {
		t.Fatalf("scheduler.New() error = %v", err)
	}
	if policy == nil {
		c, err := psa.New(nil, "")
		if err != nil {
			t.Fatalf("psa.New() error = %v", err)
		}
		policy = c
	}
	log := &eventlog.Log{}
	return New(st, a, policy, log), log, st
}

// node returns a node with room for a few small pods.
func node(name string) corev1.Node {
	alloc := corev1.ResourceList{
		corev1.ResourceCPU:    resource.MustParse("2"),
		corev1.ResourceMemory: resource.MustParse("4Gi"),
		corev1.ResourcePods:   resource.MustParse("110"),
	}
	return corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			UID:    types.UID("node-" + name),
			Labels: map[string]string{corev1.LabelHostname: name},
		},
		Status: corev1.NodeStatus{Capacity: alloc, Allocatable: alloc},
	}
}

// runningPod returns a Ready pod with the label app=web on node-a, owned by
// owner (nil for a pod without a controller).
func runningPod(name string, owner *metav1.OwnerReference) corev1.Pod {
	p := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      name,
			UID:       types.UID("pod-" + name),
			Labels:    map[string]string{"app": "web"},
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
	if owner != nil {
		p.OwnerReferences = []metav1.OwnerReference{*owner}
	}
	return p
}

// owner returns a controller reference to a workload of the given kind.
func owner(kind, name string) *metav1.OwnerReference {
	controller := true
	return &metav1.OwnerReference{
		APIVersion: "apps/v1",
		Kind:       kind,
		Name:       name,
		UID:        types.UID(kind + "-" + name),
		Controller: &controller,
	}
}

// wantKinds checks the kinds of the recorded events, in order.
func wantKinds(t *testing.T, log *eventlog.Log, want ...eventlog.Kind) {
	t.Helper()
	var got []eventlog.Kind
	for _, ev := range log.Events() {
		got = append(got, ev.Kind)
	}
	if !slices.Equal(got, want) {
		t.Errorf("event kinds = %v, want %v", got, want)
	}
}

// podNames returns the names of the pods.
func podNames(pods []*corev1.Pod) []string {
	out := make([]string, len(pods))
	for i, p := range pods {
		out[i] = p.Name
	}
	return out
}

// TestUsageProfilesAreImmutable checks that the facade returns the usage
// profiles of the snapshot (FR20) as copies, and that a drain and a Restore
// leave them unchanged, because they are not part of the Memento.
func TestUsageProfilesAreImmutable(t *testing.T) {
	want := []snapshot.UsageProfile{{Namespace: "default", Kind: "Deployment", Name: "web", Window: "168h",
		Containers: []snapshot.ContainerUsage{{Name: "app", CPUP95Millicores: 250, MemoryMaxBytes: 64 << 20}}}}
	e, _, _ := newEngine(t, &snapshot.Snapshot{
		Nodes:         []corev1.Node{node("node-a"), node("node-b")},
		Pods:          []corev1.Pod{runningPod("web-1", owner("ReplicaSet", "web-rs"))},
		UsageProfiles: want,
	}, nil)

	got := e.UsageProfiles()
	got[0].Containers[0].CPUP95Millicores = 1 // a copy; must not reach the engine

	cp := e.Save()
	if _, err := e.Drain(context.Background(), []string{"node-a"}); err != nil {
		t.Fatalf("Drain() error = %v", err)
	}
	e.Restore(cp)

	if got := e.UsageProfiles(); !reflect.DeepEqual(got, want) {
		t.Errorf("UsageProfiles() = %+v, want %+v", got, want)
	}
}

// TestSetRequestsReachesReplacements checks FR23: after SetRequests the
// replacement of an evicted pod has the new requests, so it no longer fits
// where the old pod would have fitted, and Restore brings the old requests
// back. node-b has 150m free next to a 1850m filler.
func TestSetRequestsReachesReplacements(t *testing.T) {
	web := runningPod("web-rs-aaaaa", owner("ReplicaSet", "web-rs"))
	web.GenerateName = "web-rs-"
	web.Spec.Containers[0].Resources.Limits = corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1")}
	filler := runningPod("filler-rs-aaaaa", owner("ReplicaSet", "filler-rs"))
	filler.Labels = map[string]string{"app": "filler"}
	filler.Spec.NodeName = "node-b"
	filler.Spec.Containers[0].Resources.Requests[corev1.ResourceCPU] = resource.MustParse("1850m")

	e, log, _ := newEngine(t, &snapshot.Snapshot{
		Nodes: []corev1.Node{node("node-a"), node("node-b")},
		Pods:  []corev1.Pod{web, filler},
	}, nil)
	cp := e.Save()

	req := e.WorkloadRequests("default", "ReplicaSet", "web-rs")
	app := req["app"]
	if got := app.Cpu().String(); got != "100m" {
		t.Fatalf("WorkloadRequests()[app] CPU = %s, want 100m", got)
	}
	limits := e.WorkloadLimits("default", "ReplicaSet", "web-rs")["app"]
	if got := limits.Cpu().String(); got != "1" {
		t.Errorf("WorkloadLimits()[app] CPU = %s, want 1", got)
	}
	req["app"][corev1.ResourceCPU] = resource.MustParse("5") // a copy; must not reach the engine
	again := e.WorkloadRequests("default", "ReplicaSet", "web-rs")["app"]
	if got := again.Cpu().String(); got != "100m" {
		t.Errorf("WorkloadRequests()[app] CPU after changing a copy = %s, want 100m", got)
	}
	if e.WorkloadRequests("none", "Deployment", "x") != nil {
		t.Errorf("WorkloadRequests() of an unknown workload is not nil")
	}

	if n := e.SetRequests("default", "ReplicaSet", "web-rs", "app",
		corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("500m")}); n != 1 {
		t.Fatalf("SetRequests() = %d, want 1", n)
	}
	if _, err := e.Drain(context.Background(), []string{"node-a"}); err != nil {
		t.Fatalf("Drain() error = %v", err)
	}
	wantKinds(t, log, eventlog.Cordoned, eventlog.Evicted, eventlog.Pending, eventlog.NodeDrained)

	e.Restore(cp)
	e.Detach(log)
	log = &eventlog.Log{}
	e.Attach(log)
	restored := e.WorkloadRequests("default", "ReplicaSet", "web-rs")["app"]
	if got := restored.Cpu().String(); got != "100m" {
		t.Errorf("CPU after Restore = %s, want 100m", got)
	}
	if _, err := e.Drain(context.Background(), []string{"node-a"}); err != nil {
		t.Fatalf("Drain() error = %v", err)
	}
	wantKinds(t, log, eventlog.Cordoned, eventlog.Evicted, eventlog.Placed, eventlog.NodeDrained)
}
