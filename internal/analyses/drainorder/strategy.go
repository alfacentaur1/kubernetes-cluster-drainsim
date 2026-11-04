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
	// explain the failure (FR18). Empty when Found, and possibly empty when
	// the search was Stopped.
	Failed string
	// Attempts is the number of simulated drains of one node.
	Attempts int
	// Stopped is true if the search reached its limit of attempts before
	// it found an order (Section 5.4). Order is then the longest sequence
	// found so far.
	Stopped bool
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
// attempt leaves no trace. A successful drain is kept.
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

// without returns the nodes except n, keeping their order.
func without(nodes []string, n string) []string {
	return slices.DeleteFunc(slices.Clone(nodes), func(x string) bool { return x == n })
}

// ---------------------------------------------------------------------------
// Greedy (Algorithm 5.1)
// ---------------------------------------------------------------------------

// Greedy takes, at each step, the first candidate whose drain succeeds and
// never returns to an earlier step (Algorithm 5.1). It is fast, needing at
// most n(n+1)/2 simulated drains, but it may miss an order that exists.
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
				out.Order = append(out.Order, n)  // append n to order
				remaining = without(remaining, n) // remove n from N
				found = true
				break
			}
		}
		if !found { // if not found: return failure(order)
			out.Failed = cands[0] // the first candidate explains the failure
			return out, nil
		}
	}
	out.Found = true
	return out, nil
}

// ---------------------------------------------------------------------------
// Backtracking (Algorithm 5.2)
// ---------------------------------------------------------------------------

// Backtracking proceeds like Greedy, but when no remaining node can be
// drained it returns to the previous step and tries the next candidate there
// (Algorithm 5.2). Without a limit it finds an order whenever one exists.
type Backtracking struct {
	// MaxAttempts limits the number of simulated drains; 0 means no limit
	// (Section 5.4).
	MaxAttempts int
}

// Name returns "backtracking".
func (Backtracking) Name() string { return "backtracking" }

// Search runs Algorithm 5.2. Besides the order it keeps the longest sequence
// it could drain, which the report shows when no order exists (Section 5.4).
func (b Backtracking) Search(ctx context.Context, sim engine.Simulation, nodes []string) (Outcome, error) {
	s := &search{sim: sim, max: b.MaxAttempts}
	order, err := s.backtrack(ctx, nodes, nil)
	if err != nil {
		return Outcome{Attempts: s.attempts}, err
	}
	if order != nil {
		return Outcome{Found: true, Order: order, Attempts: s.attempts}, nil
	}
	return Outcome{Order: s.best, Failed: s.bestFailed, Attempts: s.attempts, Stopped: s.stopped}, nil
}

// search holds what the recursion of Algorithm 5.2 shares across levels:
// the limit, the number of attempts and the longest sequence found so far.
type search struct {
	sim        engine.Simulation
	max        int      // limit of attempts, 0 for none
	attempts   int      // simulated drains so far
	stopped    bool     // the limit was reached
	best       []string // longest sequence drained so far
	bestFailed string   // node that could not follow best
}

// backtrack is BacktrackOrder(N, order) of Algorithm 5.2. It returns the
// complete order, or nil if no order starts with the given one.
//
// How it works: order holds the nodes drained so far, remaining the nodes
// still to drain. For each candidate n (fewest requests first):
//
//  1. Save the state.
//  2. Drain n. If it fails, restore the state and try the next candidate.
//  3. If it succeeds, call backtrack again with n added to order and removed
//     from remaining. This explores every order that starts with order + [n].
//  4. If that call finds a complete order, return it at once.
//  5. If it does not, restore the state saved in step 1 and try the next
//     candidate.
//
// The difference from Greedy is step 5: Greedy keeps a successful drain and
// never goes back, so it can get stuck. Backtracking also undoes a drain that
// succeeded when no complete order follows it, which is why the restore
// comes after the recursive call and not only after a failed drain.
//
// Example with nodes a, b, c, where c can only be drained before b:
//
//	order []      try a -> ok
//	order [a]       try b -> ok
//	order [a b]       try c -> fails      (no candidate left)
//	order [a]       restore, try c -> ok  (undo b, try c instead)
//	order [a c]       try b -> ok
//	order [a c b]   remaining is empty -> found
//
// Besides the order, the search remembers the longest sequence it could
// drain (best) and the node that could not follow it (bestFailed). When no
// order exists, the report shows them (Section 5.4, FR18). Algorithm 5.2
// itself only returns "failure" and would lose this sequence.
//
// The search stops when MaxAttempts is reached (Section 5.4), because the
// number of orders grows as n!. Every level of the recursion keeps one saved
// state, so the memory is O(n · (p + m)).
func (s *search) backtrack(ctx context.Context, remaining, order []string) ([]string, error) {
	if len(remaining) == 0 { // if N is empty: return order
		return order, nil
	}

	cands := candidates(s.sim, remaining)
	for _, n := range cands { // for n in candidates(N)
		if s.max > 0 && s.attempts >= s.max {
			s.stopped = true
			return nil, nil
		}
		s.attempts++

		cp := s.sim.Save() // state before draining n, restored below if no order follows
		res, err := s.sim.Drain(ctx, []string{n})
		if err != nil {
			return nil, err
		}
		if res.Failed == "" { // if the drain of n succeeds
			next := append(slices.Clone(order), n)
			if len(next) > len(s.best) {
				s.best, s.bestFailed = next, ""
			}
			// Explore every order that starts with order + [n].
			result, err := s.backtrack(ctx, without(remaining, n), next)
			if err != nil || result != nil {
				return result, err // an order was found, or the simulation failed
			}
			if s.stopped {
				return nil, nil
			}
		}

		// No complete order starts with order + [n] (or n could not be
		// drained): undo the drain of n and try the next candidate.
		s.sim.Restore(cp)
	}

	// No candidate can follow this sequence. If it is the longest one,
	// remember the first candidate to explain the failure (FR18).
	if slices.Equal(order, s.best) && s.bestFailed == "" {
		s.bestFailed = cands[0]
	}
	return nil, nil // return failure(order)
}
