package drainorder

import (
	"context"
	"slices"
	"testing"

	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/engine"
	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/engine/eventlog"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

// Rules for the fake simulation. canDrain says whether node can be drained
// after the nodes in drained.

// cBeforeB: c can only be drained while b is still there. Greedy drains b
// before c (b has fewer requests) and gets stuck; the only order is a, c, b.
func cBeforeB(node string, drained []string) bool {
	return node != "c" || !slices.Contains(drained, "b")
}

// aAfterB: a can only be drained after b.
func aAfterB(node string, drained []string) bool {
	return node != "a" || slices.Contains(drained, "b")
}

// cNever: c can never be drained, so no order exists.
func cNever(node string, _ []string) bool { return node != "c" }

// always: every node can be drained in any order.
func always(string, []string) bool { return true }

// --- Greedy and Backtracking (Algorithms 5.1 and 5.2) ---

// Greedy keeps a successful drain and never goes back, so it gets stuck where
// backtracking finds an order (Section 5.4).
func TestGreedyGetsStuckWhereBacktrackingFindsOrder(t *testing.T) {
	ctx := context.Background()

	g, err := Greedy{}.Search(ctx, newFakeSim(cBeforeB), []string{"a", "b", "c"})
	if err != nil {
		t.Fatalf("Greedy.Search() error = %v", err)
	}
	if g.Found || !slices.Equal(g.Order, []string{"a", "b"}) || g.Failed != "c" {
		t.Errorf("Greedy = %+v, want stuck after [a b] at c", g)
	}

	b, err := Backtracking{}.Search(ctx, newFakeSim(cBeforeB), []string{"a", "b", "c"})
	if err != nil {
		t.Fatalf("Backtracking.Search() error = %v", err)
	}
	if !b.Found || !slices.Equal(b.Order, []string{"a", "c", "b"}) {
		t.Errorf("Backtracking = %+v, want order [a c b]", b)
	}
}

// Without an order, backtracking returns the longest sequence it could drain
// and the node that could not follow it (Section 5.4, FR18).
func TestBacktrackingReportsLongestSequence(t *testing.T) {
	out, err := Backtracking{}.Search(context.Background(), newFakeSim(cNever), []string{"a", "b", "c"})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if out.Found || !slices.Equal(out.Order, []string{"a", "b"}) || out.Failed != "c" {
		t.Errorf("Search() = %+v, want longest [a b], failed c", out)
	}
}

// The search stops at the limit of attempts (Section 5.4).
func TestBacktrackingStopsAtLimit(t *testing.T) {
	out, err := Backtracking{MaxAttempts: 2}.Search(context.Background(), newFakeSim(cBeforeB), []string{"a", "b", "c"})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if out.Found || !out.Stopped || out.Attempts != 2 {
		t.Errorf("Search() = %+v, want stopped after 2 attempts", out)
	}
}

// --- Run (UC3) ---

// If the given order works, it is the answer and no search runs (UC3, steps 1–3).
func TestRunKeepsGivenOrderThatWorks(t *testing.T) {
	res, err := Run(context.Background(), newFakeSim(always), []string{"b", "a"}, Greedy{})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !res.Found || !slices.Equal(res.Order, []string{"b", "a"}) || res.GivenFailed != "" || res.Strategy != "" {
		t.Errorf("Run() = %+v, want the given order without a search", res)
	}
}

// If the given order fails, the strategy finds another one, and the events
// in the result come only from the final order, not from undone attempts.
func TestRunReplaysOnlyTheFinalOrder(t *testing.T) {
	res, err := Run(context.Background(), newFakeSim(aAfterB), []string{"a", "b"}, Greedy{})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !res.Found || res.GivenFailed != "a" || res.Strategy != "greedy" || !slices.Equal(res.Order, []string{"b", "a"}) {
		t.Errorf("Run() = %+v, want order [b a] found by greedy after the given order failed at a", res)
	}
	want := []string{"Cordoned b", "NodeDrained b", "Cordoned a", "NodeDrained a"}
	if got := describe(res.Events); !slices.Equal(got, want) {
		t.Errorf("events = %v, want %v", got, want)
	}
}

// Without an order, the replay ends with the node that failed, so the report
// can explain the failure (Section 5.4, FR18).
func TestRunExplainsMissingOrder(t *testing.T) {
	res, err := Run(context.Background(), newFakeSim(cNever), []string{"a", "b", "c"}, Backtracking{})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if res.Found || !slices.Equal(res.Drained, []string{"a", "b"}) || res.Failed != "c" {
		t.Errorf("Run() = %+v, want longest [a b], failed c", res)
	}
	if got := describe(res.Events); got[len(got)-1] != "DrainFailed c" {
		t.Errorf("last event = %q, want DrainFailed c", got[len(got)-1])
	}
}

// --- fake simulation ---

// fakeSim is a Simulation whose drains follow a simple rule instead of the
// engine. Nodes a, b and c move 100m, 200m and 300m of CPU, so the
// strategies try them in this order.
type fakeSim struct {
	canDrain  func(node string, drained []string) bool
	drained   []string
	saved     map[*engine.Checkpoint][]string
	observers []eventlog.Observer
}

// fakeSim must implement Simulation.
var _ engine.Simulation = (*fakeSim)(nil)

func newFakeSim(rule func(string, []string) bool) *fakeSim {
	return &fakeSim{canDrain: rule, saved: map[*engine.Checkpoint][]string{}}
}

func (f *fakeSim) SetPolicy(engine.PolicyChecker) {}
func (f *fakeSim) Info() engine.Info              { return engine.Info{} }

func (f *fakeSim) Attach(o eventlog.Observer) { f.observers = append(f.observers, o) }
func (f *fakeSim) Detach(o eventlog.Observer) {
	f.observers = slices.DeleteFunc(f.observers, func(x eventlog.Observer) bool { return x == o })
}

func (f *fakeSim) MoveRequests(node string) corev1.ResourceList {
	cpu := map[string]int64{"a": 100, "b": 200, "c": 300}[node]
	return corev1.ResourceList{corev1.ResourceCPU: *resource.NewMilliQuantity(cpu, resource.DecimalSI)}
}

// Save remembers which nodes are drained; Restore brings that back.
func (f *fakeSim) Save() *engine.Checkpoint {
	cp := &engine.Checkpoint{}
	f.saved[cp] = slices.Clone(f.drained)
	return cp
}
func (f *fakeSim) Restore(cp *engine.Checkpoint) { f.drained = slices.Clone(f.saved[cp]) }

// Drain drains the nodes in order until one fails, like the engine.
func (f *fakeSim) Drain(_ context.Context, nodes []string) (engine.Result, error) {
	var res engine.Result
	for _, n := range nodes {
		f.notify(eventlog.Event{Kind: eventlog.Cordoned, Node: n})
		if !f.canDrain(n, f.drained) {
			f.notify(eventlog.Event{Kind: eventlog.DrainFailed, Node: n})
			res.Failed = n
			return res, nil
		}
		f.drained = append(f.drained, n)
		f.notify(eventlog.Event{Kind: eventlog.NodeDrained, Node: n})
		res.Drained = append(res.Drained, n)
	}
	return res, nil
}

func (f *fakeSim) notify(ev eventlog.Event) {
	for _, o := range f.observers {
		o.Notify(ev)
	}
}

// describe turns events into short strings such as "Cordoned b".
func describe(events []eventlog.Event) []string {
	names := map[eventlog.Kind]string{
		eventlog.Cordoned: "Cordoned", eventlog.NodeDrained: "NodeDrained", eventlog.DrainFailed: "DrainFailed",
	}
	out := make([]string, len(events))
	for i, ev := range events {
		out[i] = names[ev.Kind] + " " + ev.Node
	}
	return out
}

func (f *fakeSim) CheckRunning(engine.PolicyChecker) []engine.Violation { return nil }
func (f *fakeSim) RemoveNode(string) error                              { return nil }
func (f *fakeSim) Requested(string) corev1.ResourceList                 { return nil }
func (f *fakeSim) Allocatable(string) corev1.ResourceList               { return nil }
