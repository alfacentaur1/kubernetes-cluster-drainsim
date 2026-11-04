package drainorder

import (
	"cmp"
	"context"
	"slices"

	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/engine"
)

// Strategy searches for an order in which all the nodes can be drained
// (Strategy pattern, Section 5.4). The greedy and backtracking strategies
// can be exchanged without changing the rest of the search.
type Strategy interface {
	// Name is shown in the report, e.g. "greedy".
	Name() string
	// Search looks for an order starting from the current state of sim.
	// It leaves sim in whatever state its last attempt produced; the
	// caller restores the state it needs afterwards.
	Search(ctx context.Context, sim engine.Simulation, nodes []string) (Outcome, error)
}

// Outcome is the result of one search.
type Outcome struct {
	// Found is true if Order contains every node.
	Found bool
	// Order is the order found, or the longest sequence of nodes that could
	// be drained when no order was found (Section 5.4).
	Order []string
	// Failed is the node whose drain failed right after Order, used to
	// explain the failure (FR18). Empty when Found.
	Failed string
	// Attempts is the number of simulated drains of one node.
	Attempts int
}

// candidates returns the nodes in the order a strategy tries them: the node
// whose drain moves the fewest requests first (Section 5.4). Requests are
// compared by CPU, then by memory, and a tie goes to the lower node name, so
// the order is deterministic (NFR7). The requests come from the current
// state, so they change after every drain.
func candidates(sim engine.Simulation, nodes []string) []string {
	type candidate struct {
		name     string
		cpu, mem int64 // millicores, bytes
	}
	cs := make([]candidate, len(nodes))
	for i, n := range nodes {
		r := sim.MoveRequests(n)
		cs[i] = candidate{name: n, cpu: r.Cpu().MilliValue(), mem: r.Memory().Value()}
	}
	slices.SortFunc(cs, func(a, b candidate) int {
		return cmp.Or(
			cmp.Compare(a.cpu, b.cpu),
			cmp.Compare(a.mem, b.mem),
			cmp.Compare(a.name, b.name),
		)
	})

	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.name
	}
	return out
}

// try simulates the drain of one node. It saves the state before the
// attempt and restores it if the drain fails (Memento pattern), so a failed
// attempt leaves no trace.
func try(ctx context.Context, sim engine.Simulation, node string) (bool, error) {
	cp := sim.Save()
	res, err := sim.Drain(ctx, []string{node})
	if err != nil {
		return false, err
	}
	if res.Failed != "" {
		sim.Restore(cp)
		return false, nil
	}
	return true, nil
}

// Greedy takes, at each step, the first candidate whose drain succeeds and
// never returns to an earlier step (Algorithm 5.1). It is fast but may miss
// an order that exists.
type Greedy struct{}

// Name returns "greedy".
func (Greedy) Name() string { return "greedy" }

// Search runs Algorithm 5.1.
func (Greedy) Search(ctx context.Context, sim engine.Simulation, nodes []string) (Outcome, error) {
	var out Outcome
	remaining := slices.Clone(nodes) // N in Algorithm 5.1

	for len(remaining) > 0 { // while N is not empty
		found := false
		cands := candidates(sim, remaining)
		for _, n := range cands { // for n in candidates(N)
			out.Attempts++
			ok, err := try(ctx, sim, n) // save, drain, restore on failure
			if err != nil {
				return out, err
			}
			if ok {
				out.Order = append(out.Order, n)                                                // append n to order
				remaining = slices.DeleteFunc(remaining, func(x string) bool { return x == n }) // remove n from N
				found = true
				break
			}
		}
		if !found { // if not found: return failure(order)
			out.Failed = cands[0] // the smallest candidate explains the failure
			return out, nil
		}
	}
	out.Found = true
	return out, nil
}
