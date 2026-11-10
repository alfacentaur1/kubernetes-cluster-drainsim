package consolidation

import (
	"context"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/engine"
	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/engine/psa"
	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/engine/state"
	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/snapshot"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

const mi = 1 << 20

// rightSizingSnapshot has one pod per case of Recommend. Every pod requests
// 64 Mi of memory (see pod).
func rightSizingSnapshot() *snapshot.Snapshot {
	capped := pod("node-a", "capped", "200m")
	capped.Spec.Containers[0].Resources.Limits = corev1.ResourceList{
		corev1.ResourceCPU:    resource.MustParse("300m"),
		corev1.ResourceMemory: resource.MustParse("128Mi"),
	}
	multi := pod("node-b", "multi", "100m")
	multi.Spec.Containers = append(multi.Spec.Containers, corev1.Container{
		Name: "sidecar", Image: "registry.k8s.io/pause:3.10",
		Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{
			corev1.ResourceCPU: resource.MustParse("50m"),
		}},
	})

	profile := func(rs string, c ...snapshot.ContainerUsage) snapshot.UsageProfile {
		return snapshot.UsageProfile{Namespace: "default", Kind: "ReplicaSet", Name: "app-" + rs, Window: "168h", Containers: c}
	}
	return &snapshot.Snapshot{
		Nodes: []corev1.Node{node("node-a"), node("node-b")},
		Pods: []corev1.Pod{
			pod("node-a", "web", "500m"),
			pod("node-a", "idle", "100m"),
			capped,
			multi,
			pod("node-b", "unprofiled", "100m"),
		},
		UsageProfiles: []snapshot.UsageProfile{
			profile("web", snapshot.ContainerUsage{Name: "app", CPUP95Millicores: 251, MemoryMaxBytes: 100_000_000}),
			profile("idle", snapshot.ContainerUsage{Name: "app"}),
			profile("capped", snapshot.ContainerUsage{Name: "app", CPUP95Millicores: 800, MemoryMaxBytes: 200 * mi}),
			profile("multi", snapshot.ContainerUsage{Name: "app", CPUP95Millicores: 20, MemoryMaxBytes: 10 * mi}),
			profile("gone", snapshot.ContainerUsage{Name: "app", CPUP95Millicores: 20, MemoryMaxBytes: 10 * mi}),
		},
	}
}

// TestRecommend checks FR21 and FR22 with numbers computed by hand.
func TestRecommend(t *testing.T) {
	eng := newTestEngine(t, rightSizingSnapshot())

	got := Recommend(eng, 0.15)

	rec := func(rs, container string, curCPU, curMem, cpu, mem int64) Recommendation {
		return Recommendation{Namespace: "default", Kind: "ReplicaSet", Name: "app-" + rs, Container: container,
			CurrentCPUMillicores: curCPU, CurrentMemoryBytes: curMem,
			RecommendedCPUMillicores: cpu, RecommendedMemoryBytes: mem}
	}
	// capped: p95 800m and 200 Mi · 1.15 = 230 Mi are above the limits
	// 300m and 128 Mi; both are above the requests 200m and 64 Mi.
	capped := rec("capped", "app", 200, 64*mi, 300, 128*mi)
	capped.Capped, capped.Higher = true, true
	// idle: no measurable usage gives the minimums 1m and 1 Mi.
	idle := rec("idle", "app", 100, 64*mi, 1, 1*mi)
	// multi: 10 Mi · 1.15 = 11.5 Mi, rounded up to 12 Mi; the sidecar has
	// no usage data and keeps its requests (50m, no memory request).
	multiApp := rec("multi", "app", 100, 64*mi, 20, 12*mi)
	sidecar := rec("multi", "sidecar", 50, 0, 50, 0)
	sidecar.NoData = true
	// web: 100 000 000 B · 1.15 = 115 000 000 B = 109.67 Mi, rounded up
	// to 110 Mi; CPU is the p95 251m.
	web := rec("web", "app", 500, 64*mi, 251, 110*mi)
	web.Higher = true // 110 Mi > 64 Mi, although CPU goes down
	// "gone" has no running pod and "unprofiled" has no profile: neither
	// appears.
	want := []Recommendation{capped, idle, multiApp, sidecar, web}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("Recommend() =\n%+v\nwant\n%+v", got, want)
	}
}

// A margin of 0 takes the maximum itself, rounded up to whole Mi.
func TestRecommendWithoutMargin(t *testing.T) {
	eng := newTestEngine(t, rightSizingSnapshot())
	for _, r := range Recommend(eng, 0) {
		if r.Name == "app-web" && r.RecommendedMemoryBytes != 96*mi { // 95.37 Mi → 96 Mi
			t.Errorf("web memory = %d, want %d (96 Mi)", r.RecommendedMemoryBytes, 96*mi)
		}
	}
}

// Without usage profiles there is nothing to recommend.
func TestRecommendWithoutProfiles(t *testing.T) {
	if got := Recommend(newTestEngine(t, threeNodes()), 0.15); got != nil {
		t.Errorf("Recommend() = %+v, want nil", got)
	}
}

// Apply sets the recommendations in the cluster state and skips containers
// without usage data (FR23).
func TestApply(t *testing.T) {
	eng := newTestEngine(t, rightSizingSnapshot())

	if n := Apply(eng, Recommend(eng, 0.15)); n != 4 {
		t.Errorf("Apply() = %d, want 4 (one pod per container with data)", n)
	}
	multi := eng.WorkloadRequests("default", "ReplicaSet", "app-multi")
	app, sidecar := multi["app"], multi["sidecar"]
	if app.Cpu().MilliValue() != 20 || app.Memory().Value() != 12*mi {
		t.Errorf("multi/app requests = %v, want 20m and 12Mi", app)
	}
	if sidecar.Cpu().MilliValue() != 50 || !sidecar.Memory().IsZero() {
		t.Errorf("multi/sidecar requests = %v, want unchanged 50m", sidecar)
	}
}

// Scenario F (consolidation with right-sizing, FR23): three nodes of 2 CPU
// with 500m, 1200m and 1200m requested; with the current requests only
// node-a can be removed. The two large pods use 600m (p95), so after the
// right-sizing node-a and node-b go and 500m + 600m + 600m = 1700m fit on
// node-c alone.
func TestRightSizingFreesAnotherNode(t *testing.T) {
	snap := threeNodes()
	for _, rs := range []string{"large-b", "large-c"} {
		snap.UsageProfiles = append(snap.UsageProfiles, snapshot.UsageProfile{
			Namespace: "default", Kind: "ReplicaSet", Name: "app-" + rs, Window: "168h",
			Containers: []snapshot.ContainerUsage{{Name: "app", CPUP95Millicores: 600, MemoryMaxBytes: 32 * mi}},
		})
	}
	eng := newTestEngine(t, snap)
	nodes := []string{"node-a", "node-b", "node-c"}
	cp := eng.Save()

	before, err := Run(context.Background(), eng, nodes, nil)
	if err != nil {
		t.Fatal(err)
	}
	eng.Restore(cp)
	Apply(eng, Recommend(eng, 0.15))
	after, err := Run(context.Background(), eng, nodes, nil)
	if err != nil {
		t.Fatal(err)
	}

	if !slices.Equal(before.Removed, []string{"node-a"}) || before.LowerBound != 2 {
		t.Errorf("before: Removed = %v, LowerBound = %d, want [node-a], 2", before.Removed, before.LowerBound)
	}
	if !slices.Equal(after.Removed, []string{"node-a", "node-b"}) || after.LowerBound != 1 {
		t.Errorf("after: Removed = %v, LowerBound = %d, want [node-a node-b], 1", after.Removed, after.LowerBound)
	}
}

// newTestEngine builds an engine over the snapshot with the real scheduler
// adapter and the levels of the namespaces.
func newTestEngine(t *testing.T, snap *snapshot.Snapshot) *engine.Engine {
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
	return engine.New(state.New(snap), a, policy)
}
