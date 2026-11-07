package consolidation

import (
	"cmp"
	"context"
	"fmt"
	"slices"

	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/engine"
	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/engine/eventlog"
	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/estimator"
	corev1 "k8s.io/api/core/v1"
)

// CostModel estimates what removing a node saves (ICostModel, Strategy
// pattern, Section 5.5). The consolidation does not depend on how prices and
// emissions are obtained; estimator.Estimator is the implementation.
type CostModel interface {
	Estimate(node *corev1.Node) estimator.Saving
}

// Result is the outcome of the consolidation (component Consolidation,
// Section 5.5).
type Result struct {
	// Nodes are the candidates in the order they were tried.
	Nodes []string
	// Removed are the nodes that can be removed while all pods still run
	// (FR24), in the order they were removed (FR25).
	Removed []string
	// Kept are the nodes that cannot be removed, with the reason.
	Kept []Kept
	// LowerBound is the smallest number of nodes any solution needs
	// (Section 5.5). The greedy procedure may keep more.
	LowerBound int
	// Savings holds one estimate per removed node, in removal order. It is
	// empty if no cost model is given.
	Savings []estimator.Saving
	// Events are the decisions of the drains of the removed nodes.
	Events []eventlog.Event
	// Info describes the snapshot (Section 5.7).
	Info engine.Info
}

// Kept is a node that cannot be removed.
type Kept struct {
	Node   string `json:"node"`
	Reason string `json:"reason"`
}

// Run follows Algorithm 5.3. It tries the nodes one by one, starting with the
// node whose pods reserve the least. A node is removed if its drain succeeds
// and every replacement is placed, because FR24 requires all pods to keep
// running; otherwise the state is restored and the node stays. Removal is a
// simulated drain, so PodDisruptionBudgets, placement rules and Pod Security
// levels apply as in Q1 (FR25). If cost is not nil, the saving of every
// removed node is estimated (FR26).
func Run(ctx context.Context, sim engine.Simulation, nodes []string, cost CostModel) (Result, error) {
	res := Result{Info: sim.Info(), LowerBound: lowerBound(sim, nodes)}

	// N sorted by the requests of its pods (ascending), once at the start.
	res.Nodes = byRequests(sim, nodes)

	for _, n := range res.Nodes {
		ok, events, reason, err := attempt(ctx, sim, n)
		if err != nil {
			return Result{}, err
		}
		if !ok {
			res.Kept = append(res.Kept, Kept{Node: n, Reason: reason})
			continue
		}
		// The node is estimated before it is removed: afterwards sim.Node
		// returns nil and its labels and capacity are gone.
		if cost != nil {
			res.Savings = append(res.Savings, cost.Estimate(sim.Node(n)))
		}
		// remove n from the cluster state, append n to removed
		if err := sim.RemoveNode(n); err != nil {
			return Result{}, err
		}
		res.Removed = append(res.Removed, n)
		res.Events = append(res.Events, events...)
	}
	return res, nil
}

// attempt simulates the drain of one node and keeps it only if all pods
// still run afterwards. A successful attempt is never undone (Algorithm 5.3
// does not go back), so its events are final; a failed one is restored and
// only its reason is kept.
func attempt(ctx context.Context, sim engine.Simulation, node string) (bool, []eventlog.Event, string, error) {
	s := sim.Save() // s ← save the state
	log := &eventlog.Log{}
	sim.Attach(log)
	res, err := sim.Drain(ctx, []string{node})
	sim.Detach(log)
	if err != nil {
		return false, nil, "", err
	}
	events := log.Events()

	reason := ""
	if res.Failed != "" {
		reason = failureReason(events)
	} else {
		reason = unplacedReason(events)
	}
	if reason != "" {
		sim.Restore(s) // restore the state s
		return false, nil, reason, nil
	}
	return true, events, "", nil
}

// failureReason returns the reason of the failed drain, from its last event.
func failureReason(events []eventlog.Event) string {
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Kind == eventlog.DrainFailed {
			return "the drain fails: " + events[i].Reason
		}
	}
	return "the drain fails"
}

// unplacedReason returns why a replacement would not run after the drain,
// or "" if all replacements were placed.
func unplacedReason(events []eventlog.Event) string {
	for _, ev := range events {
		switch ev.Kind {
		case eventlog.Pending:
			return fmt.Sprintf("replacement %s would stay pending: %s", ev.Pod, ev.Reason)
		case eventlog.LostReplica:
			return fmt.Sprintf("replacement %s would be lost: %s", ev.Pod, ev.Reason)
		}
	}
	return ""
}

// byRequests returns the nodes sorted by the requests of their pods: CPU,
// then memory, then name (the same order as in Section 5.4, NFR7).
func byRequests(sim engine.Simulation, nodes []string) []string {
	type node struct {
		name     string
		cpu, mem int64
	}
	ns := make([]node, len(nodes))
	for i, n := range nodes {
		r := sim.Requested(n)
		ns[i] = node{name: n, cpu: r.Cpu().MilliValue(), mem: r.Memory().Value()}
	}
	slices.SortFunc(ns, func(a, b node) int {
		return cmp.Or(cmp.Compare(a.cpu, b.cpu), cmp.Compare(a.mem, b.mem), cmp.Compare(a.name, b.name))
	})
	out := make([]string, len(ns))
	for i, n := range ns {
		out[i] = n.name
	}
	return out
}

// lowerBound returns the total requests of the pods on the nodes divided by
// the allocatable resources of one node, rounded up (Section 5.5). It is
// computed for CPU and memory and the larger number is taken. The largest
// node is used, so the bound also holds when nodes differ in size; for
// nodes of one size it is exactly the formula of Section 5.5.
func lowerBound(sim engine.Simulation, nodes []string) int {
	var cpu, mem, maxCPU, maxMem int64
	for _, n := range nodes {
		r := sim.Requested(n)
		cpu += r.Cpu().MilliValue()
		mem += r.Memory().Value()
		a := sim.Allocatable(n)
		maxCPU = max(maxCPU, a.Cpu().MilliValue())
		maxMem = max(maxMem, a.Memory().Value())
	}
	return max(ceilDiv(cpu, maxCPU), ceilDiv(mem, maxMem))
}

// ceilDiv returns a / b rounded up, or 0 if b is 0.
func ceilDiv(a, b int64) int {
	if b == 0 {
		return 0
	}
	return int((a + b - 1) / b)
}
