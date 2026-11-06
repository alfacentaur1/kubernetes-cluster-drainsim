package policyimpact

import (
	"context"
	"slices"

	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/engine"
	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/engine/eventlog"
)

// Result is the outcome of the policy impact analysis (component Policy
// impact, Section 5.6).
type Result struct {
	// Nodes are the nodes drained in both runs, in the given order (FR5).
	Nodes []string
	// Before and After are the drains without and with the target levels.
	Before, After engine.Result
	// Impact is the step that succeeds without the target levels but fails
	// with them, or nil if the target levels do not affect the drain.
	Impact *Impact
	// Lost are the replicas the target levels reject but the current levels
	// do not (FR31). Each keeps its budget reduced (FR32), so it explains an
	// Impact; without an Impact the application still loses the replica (S2).
	Lost []LostReplica
	// Running are the workloads whose running pods violate the target
	// levels (FR30). They are not affected now, but on their next restart.
	Running []engine.Violation
	// Events are the decisions of the run with the target levels.
	Events []eventlog.Event
	// Info describes the snapshot (Section 5.7).
	Info engine.Info
}

// Impact is the drain step that fails only because of the target levels.
type Impact struct {
	Step int    `json:"step"` // number of the step, from 1
	Node string `json:"node"` // node whose drain fails
}

// LostReplica is a replacement that the target levels reject (FR31).
type LostReplica struct {
	Step   int    `json:"step"`   // step in which the replacement was rejected
	Node   string `json:"node"`   // node that was drained in that step
	Pod    string `json:"pod"`    // the rejected replacement
	Reason string `json:"reason"` // level and violated controls (S3)
}

// Run follows Algorithm 5.4. current checks pods against the levels set on
// the namespaces, target against the target levels (FR29). Both runs drain
// the same nodes in the same order from the same state, so they differ only
// in the levels, and a step that fails only in the second run fails because
// of the new levels (Section 5.6).
func Run(ctx context.Context, sim engine.Simulation, nodes []string, current, target engine.PolicyChecker) (Result, error) {
	res := Result{Nodes: nodes, Info: sim.Info()}

	// violations ← running pods violating targets, grouped by workload
	res.Running = sim.CheckRunning(target)

	// s ← save the state
	s := sim.Save()

	// before ← simulate the drain of order
	sim.SetPolicy(current)
	before, beforeEvents, err := drain(ctx, sim, nodes)
	if err != nil {
		return Result{}, err
	}

	// restore the state s
	sim.Restore(s)

	// after ← simulate the drain of order with targets
	sim.SetPolicy(target)
	after, afterEvents, err := drain(ctx, sim, nodes)
	if err != nil {
		return Result{}, err
	}
	res.Before, res.After, res.Events = before, after, afterEvents
	res.Lost = lostOnlyAfter(nodes, beforeEvents, afterEvents)

	// impact ← steps that succeed in before but fail in after. A drain
	// stops at its first failed step, so there is at most one such step.
	if after.Failed != "" && slices.Contains(before.Drained, after.Failed) {
		res.Impact = &Impact{Step: slices.Index(nodes, after.Failed) + 1, Node: after.Failed}
	}
	return res, nil
}

// lostOnlyAfter returns the replicas lost in the run with the target levels
// but not in the run without them.
func lostOnlyAfter(nodes []string, before, after []eventlog.Event) []LostReplica {
	lostBefore := map[string]bool{}
	for _, ev := range before {
		if ev.Kind == eventlog.LostReplica {
			lostBefore[ev.Node+"|"+ev.Pod] = true
		}
	}
	var out []LostReplica
	for _, ev := range after {
		if ev.Kind == eventlog.LostReplica && !lostBefore[ev.Node+"|"+ev.Pod] {
			out = append(out, LostReplica{Step: slices.Index(nodes, ev.Node) + 1,
				Node: ev.Node, Pod: ev.Pod, Reason: ev.Reason})
		}
	}
	return out
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
