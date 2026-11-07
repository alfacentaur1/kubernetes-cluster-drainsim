package reporter

import (
	"fmt"
	"strings"
	"time"

	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/analyses/drainorder"
	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/engine"
	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/engine/eventlog"
)

// FormatVersion is the version of the JSON report, so that tools reading it
// can detect changes (Section 5.7).
const FormatVersion = "drainsim.report/v1"

// Search describes the search for another order after the given order
// failed (UC3, UC5).
type Search struct {
	GivenFailed string `json:"givenOrderFailedAt"` // node where the given order failed
	Strategy    string `json:"strategy,omitempty"` // "greedy" or "backtracking", empty if none ran
	Attempts    int    `json:"attempts"`           // simulated drains of one node
	Stopped     bool   `json:"stopped"`            // the limit of attempts was reached
}

// Report is the result of one command in the structure of Section 5.7: a
// summary, the result of each simulated step and the cause of a failure.
type Report struct {
	FormatVersion string        `json:"formatVersion"`
	Command       string        `json:"command"`
	Passed        bool          `json:"passed"`
	Summary       string        `json:"summary"`
	Order         []string      `json:"order"`   // the order that was checked
	Drained       []string      `json:"drained"` // nodes that could be drained (Section 5.4)
	Steps         []Step        `json:"steps"`
	Failure       *Failure      `json:"failure,omitempty"`
	Snapshot      Snapshot      `json:"snapshot"`
	Nodes         []string      `json:"nodes"`            // nodes to drain, in the given order
	Search        *Search       `json:"search,omitempty"` // present when the given order failed
	Policy        *PolicyImpact `json:"policy,omitempty"` // only in the report of the policy command
	// Consolidation is only in the report of the consolidate command.
	Consolidation *Consolidation `json:"consolidation,omitempty"`
}

// Snapshot describes the snapshot the simulation ran on, shown at the end of
// the report (Section 5.7), so the reader can judge how current the result is
// and for which pods it may differ from a real drain.
type Snapshot struct {
	CreatedAt         time.Time        `json:"createdAt"`
	Age               string           `json:"age"` // time between CreatedAt and the report, e.g. "26h3m0s"
	KubernetesVersion string           `json:"kubernetesVersion"`
	Warnings          []engine.Warning `json:"warnings,omitempty"`
}

// Step is the result of draining one node.
type Step struct {
	Number       int           `json:"step"`
	Node         string        `json:"node"`
	Drained      bool          `json:"drained"`
	Evicted      []string      `json:"evicted,omitempty"`
	Replacements []Replacement `json:"replacements,omitempty"`
	Skipped      []PodReason   `json:"skipped,omitempty"`
	Refused      []PodReason   `json:"refused,omitempty"`
	Blocked      []PodReason   `json:"blocked,omitempty"` // evictions the budget never allowed
}

// Replacement tells what happened to the replacement of an evicted pod.
type Replacement struct {
	Evicted string `json:"evicted"`          // the evicted pod
	Pod     string `json:"pod"`              // its replacement
	Outcome string `json:"outcome"`          // "placed", "pending" or "lost"
	Node    string `json:"node,omitempty"`   // where it was placed
	Reason  string `json:"reason,omitempty"` // why it was not placed (FR19, S2)
}

// PodReason is a pod together with the reason of a decision about it.
type PodReason struct {
	Pod    string `json:"pod"`
	Reason string `json:"reason"`
}

// Failure names what blocked the drain: the step, the node, the cause and
// the pods with the rule or budget that stopped them (FR18).
type Failure struct {
	Step  int         `json:"step"`
	Node  string      `json:"node"`
	Cause string      `json:"cause"` // "budget" or "refused"
	Pods  []PodReason `json:"pods"`
	// Unplaced are replacements in the failed step that did not restore
	// their budget, which often explains a blocked eviction (P7, S2).
	Unplaced []Replacement `json:"unplaced,omitempty"`
}

// Drain builds the report of the drain command from the result of the drain
// order search. now is the current time, for the age of the snapshot.
func Drain(res drainorder.Result, now time.Time) Report {
	r := Report{
		FormatVersion: FormatVersion,
		Command:       "drain",
		Passed:        res.Found,
		Nodes:         res.Nodes,
		Order:         res.Order,
		Drained:       res.Drained,
		Steps:         steps(res.Events),
		Snapshot:      snapshotInfo(res.Info, now),
	}
	if res.GivenFailed != "" {
		r.Search = &Search{GivenFailed: res.GivenFailed, Strategy: res.Strategy,
			Attempts: res.Attempts, Stopped: res.Stopped}
	}
	r.Summary = summary(res)
	if !res.Found && res.Failed != "" {
		r.Failure = failure(r.Steps, res.Failed)
	}
	return r
}

// summary says in one or two sentences whether the nodes can be drained and
// how the answer was found (Section 5.7).
func summary(res drainorder.Result) string {
	n := len(res.Nodes)
	switch {
	case res.Found && res.GivenFailed == "":
		return fmt.Sprintf("All %d nodes can be drained in the given order: %s.",
			n, strings.Join(res.Order, ", "))
	case res.Found:
		return fmt.Sprintf("The given order fails at node %s. The %s search found an order in which all %d nodes can be drained: %s.",
			res.GivenFailed, res.Strategy, n, strings.Join(res.Order, ", "))
	}

	s := fmt.Sprintf("The nodes cannot be drained in the given order (it fails at node %s).", res.GivenFailed)
	switch {
	case res.Strategy == "":
		// no search ran
	case res.Stopped:
		s += fmt.Sprintf(" The %s search stopped after %d attempts without finding an order.", res.Strategy, res.Attempts)
	default:
		s += fmt.Sprintf(" The %s search found no order either.", res.Strategy)
	}
	if len(res.Drained) > 0 {
		s += fmt.Sprintf(" The longest sequence that can be drained is: %s.", strings.Join(res.Drained, ", "))
	}
	return s
}

// snapshotInfo describes the snapshot at the time now.
func snapshotInfo(info engine.Info, now time.Time) Snapshot {
	return Snapshot{
		CreatedAt:         info.CreatedAt,
		Age:               now.Sub(info.CreatedAt).Round(time.Second).String(),
		KubernetesVersion: info.KubernetesVersion,
		Warnings:          info.Warnings,
	}
}

// steps groups the events by drained node, in the order of the drain.
func steps(events []eventlog.Event) []Step {
	// Pods that were evicted in the end; their earlier deferrals are not blocks.
	evicted := map[string]bool{}
	for _, ev := range events {
		if ev.Kind == eventlog.Evicted {
			evicted[ev.Node+"|"+ev.Pod] = true
		}
	}

	var out []Step
	at := map[string]int{}        // node -> index in out
	blockedAt := map[string]int{} // node|pod -> index in Step.Blocked
	lastEvicted := ""             // the pod whose replacement comes next
	for _, ev := range events {
		i, ok := at[ev.Node]
		if !ok {
			out = append(out, Step{Number: len(out) + 1, Node: ev.Node})
			i = len(out) - 1
			at[ev.Node] = i
		}
		s := &out[i]

		switch ev.Kind {
		case eventlog.Skipped:
			s.Skipped = append(s.Skipped, PodReason{Pod: ev.Pod, Reason: ev.Reason})
		case eventlog.Refused:
			s.Refused = append(s.Refused, PodReason{Pod: ev.Pod, Reason: ev.Reason})
		case eventlog.Deferred:
			k := ev.Node + "|" + ev.Pod
			if evicted[k] {
				continue // allowed in a later round
			}
			if j, ok := blockedAt[k]; ok {
				s.Blocked[j].Reason = ev.Reason // keep the reason of the last round
				continue
			}
			blockedAt[k] = len(s.Blocked)
			s.Blocked = append(s.Blocked, PodReason{Pod: ev.Pod, Reason: ev.Reason})
		case eventlog.Evicted:
			s.Evicted = append(s.Evicted, ev.Pod)
			lastEvicted = ev.Pod
		case eventlog.Placed:
			s.Replacements = append(s.Replacements, Replacement{Evicted: lastEvicted, Pod: ev.Pod, Outcome: "placed", Node: ev.Target})
		case eventlog.Pending:
			s.Replacements = append(s.Replacements, Replacement{Evicted: lastEvicted, Pod: ev.Pod, Outcome: "pending", Reason: ev.Reason})
		case eventlog.LostReplica:
			s.Replacements = append(s.Replacements, Replacement{Evicted: lastEvicted, Pod: ev.Pod, Outcome: "lost", Reason: ev.Reason})
		case eventlog.NodeDrained:
			s.Drained = true
		}
	}
	return out
}

// failure explains why the drain of node failed, in the same order as the
// engine decides it: pods blocked by a budget first, then refused pods.
func failure(steps []Step, node string) *Failure {
	for _, s := range steps {
		if s.Node != node {
			continue
		}
		f := &Failure{Step: s.Number, Node: s.Node}
		if len(s.Blocked) > 0 {
			f.Cause, f.Pods = "budget", s.Blocked
			for _, r := range s.Replacements {
				if r.Outcome != "placed" {
					f.Unplaced = append(f.Unplaced, r)
				}
			}
		} else {
			f.Cause, f.Pods = "refused", s.Refused
		}
		return f
	}
	return nil
}
