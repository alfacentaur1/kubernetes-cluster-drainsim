package drainorder

import (
	"context"

	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/engine"
)

// Result is the outcome of the drain order search (component Drain order
// search, Section 5.4).
type Result struct {
	// Found is true if every node can be drained in Order.
	Found bool
	// Order is the order that was tried (FR17 when Found).
	Order []string
	// Drained is the longest sequence of nodes that could be drained
	// (Section 5.4); it equals Order when Found.
	Drained []string
	// Failed is the node whose drain failed, empty when Found.
	Failed string
}

// Run checks whether the nodes can be drained in the given order (UC3).
// Searching for another order when this one fails (UC5, FR16) is added
// with the greedy and backtracking strategies.
func Run(ctx context.Context, sim engine.Simulation, order []string) (Result, error) {
	res, err := sim.Drain(ctx, order)
	if err != nil {
		return Result{}, err
	}
	return Result{
		Found:   res.Failed == "",
		Order:   order,
		Drained: res.Drained,
		Failed:  res.Failed,
	}, nil
}
