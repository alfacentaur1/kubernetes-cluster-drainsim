package reporter

import (
	"fmt"
	"time"

	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/analyses/policyimpact"
	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/engine"
)

// PolicyImpact is the part of a report that only the policy command has
// (Section 5.6, Figure 5.6).
type PolicyImpact struct {
	// Impact is the step that fails only with the target levels (S3).
	Impact *policyimpact.Impact `json:"impact,omitempty"`
	// Lost are the replicas lost only with the target levels (FR31, S2).
	Lost []policyimpact.LostReplica `json:"lost,omitempty"`
	// FailsAlready is the node where the drain fails even with the current
	// levels; such a failure is not caused by the target levels.
	FailsAlready string `json:"failsAlready,omitempty"`
	// Running are the workloads whose running pods violate the target
	// levels and would be rejected on their next restart (FR30, S1).
	Running []engine.Violation `json:"running,omitempty"`
}

// Policy builds the report of the policy command. The steps show the drain
// with the target levels. The check passes when the target levels do not
// make any step fail (Algorithm 5.4).
func Policy(res policyimpact.Result, now time.Time) Report {
	r := Report{
		FormatVersion: FormatVersion,
		Command:       "policy",
		Passed:        res.Impact == nil,
		Nodes:         res.Nodes,
		Drained:       res.After.Drained,
		Steps:         steps(res.Events),
		Snapshot:      snapshotInfo(res.Info, now),
		Policy: &PolicyImpact{
			Impact:       res.Impact,
			Lost:         res.Lost,
			FailsAlready: res.Before.Failed,
			Running:      res.Running,
		},
	}
	if res.Impact != nil {
		r.Failure = failure(r.Steps, res.Impact.Node)
	}
	r.Summary = policySummary(res)
	return r
}

// policySummary says whether the target levels affect the drain, following
// the two outcomes of Algorithm 5.4.
func policySummary(res policyimpact.Result) string {
	n := len(res.Nodes)
	var s string
	switch {
	case res.Impact != nil:
		s = fmt.Sprintf("With the target levels, the drain fails at node %s (step %d of %d), which it passes with the current levels.",
			res.Impact.Node, res.Impact.Step, n)
	case len(res.Lost) > 0:
		s = fmt.Sprintf("The target levels do not make the drain of the %d nodes fail, but %d replicas would be lost.",
			n, len(res.Lost))
	default:
		s = fmt.Sprintf("The target levels do not affect the drain of the %d nodes.", n)
	}
	if res.Before.Failed != "" {
		s += fmt.Sprintf(" The drain fails at node %s already with the current levels.", res.Before.Failed)
	}
	if len(res.Running) > 0 {
		s += fmt.Sprintf(" %d workloads have running pods that violate the target levels.", len(res.Running))
	}
	return s
}
