package drainorder

import (
	"context"
	"fmt"
	"slices"

	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/engine"
	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/engine/eventlog"
)

// Result is the outcome of the drain order search (component Drain order
// search, Section 5.4).
type Result struct {
	// Nodes are the nodes to drain, in the order the user gave (FR5).
	Nodes []string
	// Found is true if all the nodes can be drained in Order.
	Found bool
	// Order is an order in which all the nodes can be drained (FR17).
	// Empty when nothing was found.
	Order []string
	// Drained is the longest sequence of nodes that could be drained
	// (Section 5.4). It equals Order when Found.
	Drained []string
	// Failed is the node that could not be drained after Drained, empty
	// when Found (FR18).
	Failed string

	// GivenFailed is the node at which the given order failed, empty if the
	// given order worked and no search was needed (UC3).
	GivenFailed string
	// Strategy is the name of the strategy that searched for another order,
	// empty if no search ran.
	Strategy string
	// Attempts is the number of simulated drains of one node in the search.
	Attempts int
	// Stopped is true if the search reached its limit of attempts.
	Stopped bool

	// Events are the decisions of the drain shown in the report: of the
	// order found, or of the longest sequence and the node that failed after
	// it. Events of the attempts the search undid are not included.
	Events []eventlog.Event
	// Info describes the snapshot (Section 5.7).
	Info engine.Info
}

// Run answers Q1 for the nodes, following the main scenario of UC3:
//
//  1. Simulate the nodes in the given order. If all can be drained, that is
//     the answer.
//  2. Otherwise return to the starting state and let the strategy search for
//     another order (UC5, FR16). A nil strategy skips the search.
//  3. Return to the starting state again and drain the result once more with
//     an event log attached, so the report shows only the final drain.
//
// The replay in step 3 gives the same result as the search, because the
// simulation is deterministic (NFR7) and a checkpoint also restores the
// names of replacements.
func Run(ctx context.Context, sim engine.Simulation, nodes []string, strategy Strategy) (Result, error) {
	res := Result{Nodes: nodes, Info: sim.Info()}
	start := sim.Save()

	// 1. The given order, with the event log attached.
	given, events, err := drain(ctx, sim, nodes)
	if err != nil {
		return Result{}, err
	}
	if given.Failed == "" {
		res.Found, res.Order, res.Drained, res.Events = true, nodes, nodes, events
		return res, nil
	}
	res.GivenFailed = given.Failed
	if strategy == nil {
		res.Drained, res.Failed, res.Events = given.Drained, given.Failed, events
		return res, nil
	}

	// 2. Search for another order, from the starting state and without the
	//    event log, because most attempts are undone.
	sim.Restore(start)
	out, err := strategy.Search(ctx, sim, nodes)
	if err != nil {
		return Result{}, err
	}
	res.Strategy, res.Attempts, res.Stopped = strategy.Name(), out.Attempts, out.Stopped

	// 3. Replay the result with the event log. Without an order, replay the
	//    longest sequence followed by the node that could not be drained, so
	//    the report can explain why (Section 5.4, FR18).
	sim.Restore(start)
	replay := slices.Clone(out.Order)
	if !out.Found && out.Failed != "" {
		replay = append(replay, out.Failed)
	}
	final, events, err := drain(ctx, sim, replay)
	if err != nil {
		return Result{}, err
	}
	if final.Failed != out.Failed {
		return Result{}, fmt.Errorf("replaying the order %v gave a different result than the search", replay)
	}

	res.Found, res.Drained, res.Failed, res.Events = out.Found, out.Order, out.Failed, events
	if out.Found {
		res.Order = out.Order
	}
	return res, nil
}

// drain simulates the nodes in order with a new event log attached and
// returns the result together with the recorded events.
func drain(ctx context.Context, sim engine.Simulation, nodes []string) (engine.Result, []eventlog.Event, error) {
	log := &eventlog.Log{}
	sim.Attach(log)
	defer sim.Detach(log)
	res, err := sim.Drain(ctx, nodes)
	return res, log.Events(), err
}
