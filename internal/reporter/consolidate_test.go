package reporter

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/analyses/consolidation"
	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/engine"
	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/estimator"
)

// The summary gives the removable nodes, the lower bound and the total of the
// known savings, and says how many removed nodes have an unknown saving
// (Section 5.7).
func TestConsolidateSummaryWithKnownAndUnknownSavings(t *testing.T) {
	rep := Consolidate(result(), "EUR", 730, now)

	want := "2 of 5 nodes can be removed while all pods still run; at least 3 nodes are needed. " +
		"Saving per month: 306.60 EUR, 5.1 kg CO₂e (1 node unknown)."
	if rep.Summary != want {
		t.Errorf("Summary =\n  %q\nwant\n  %q", rep.Summary, want)
	}
	if !rep.Passed || rep.Command != "consolidate" {
		t.Errorf("Passed, Command = %v, %q, want true, consolidate", rep.Passed, rep.Command)
	}
	tot := rep.Consolidation.Total
	if tot == nil || tot.MoneyUnknown != 0 || tot.CO2Unknown != 1 {
		t.Errorf("Total = %+v, want money known for all, CO₂ unknown for 1", tot)
	}
}

// Without a cost model the savings are unknown and only the removable nodes
// are reported (Section 5.5).
func TestConsolidateSummaryWithoutCostModel(t *testing.T) {
	res := result()
	res.Savings = nil

	rep := Consolidate(res, "", 0, now)

	want := "2 of 5 nodes can be removed while all pods still run; at least 3 nodes are needed. " +
		"The savings are unknown: the configuration has no estimate."
	if rep.Summary != want {
		t.Errorf("Summary = %q, want %q", rep.Summary, want)
	}
	if rep.Consolidation.Total != nil {
		t.Errorf("Total = %+v, want nil without a cost model", rep.Consolidation.Total)
	}
	text := render(t, rep)
	if !strings.Contains(text, "Savings: unknown (no estimate section in the configuration)") {
		t.Errorf("Text() =\n%s\nwant the savings marked unknown", text)
	}
}

// When nothing can be removed, the summary says so and the run still passes:
// the consolidation is an estimate, not a check.
func TestConsolidateSummaryNothingRemoved(t *testing.T) {
	res := consolidation.Result{
		Nodes:      []string{"node-a"},
		Kept:       []consolidation.Kept{{Node: "node-a", Reason: "the drain fails: x"}},
		LowerBound: 1,
	}

	rep := Consolidate(res, "EUR", 730, now)

	want := "0 of 1 nodes can be removed while all pods still run; at least 1 node is needed."
	if rep.Summary != want || !rep.Passed {
		t.Errorf("Summary, Passed = %q, %v, want %q, true", rep.Summary, rep.Passed, want)
	}
}

// The text report lists removable and kept nodes, the lower bound and the
// savings per node and in total, with "unknown" where a value is missing.
func TestConsolidateText(t *testing.T) {
	text := render(t, Consolidate(result(), "EUR", 730, now))

	for _, want := range []string{
		"PASSED  2 of 5 nodes",
		"Removable nodes, in order: node-a, node-b\n",
		"Kept nodes:\n  node-c: replacement default/web-sim0 would stay pending: no node fits\n",
		"Lower bound: at least 3 nodes are needed\n",
		"Savings per month:\n",
		"  node-a (Standard_D4s_v5, germanywestcentral): 153.30 EUR, 8.1 kWh, 5.1 kg CO₂e\n",
		"  node-b (Standard_D4s_v5, unknown): 153.30 EUR, CO₂e unknown\n",
		"  total: 306.60 EUR, 8.1 kWh, 5.1 kg CO₂e (money unknown for 0 nodes, CO₂ unknown for 1 node)\n",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("Text() =\n%s\nwant substring %q", text, want)
		}
	}
}

// The JSON report carries the consolidation section with the savings.
func TestConsolidateJSON(t *testing.T) {
	var buf bytes.Buffer
	if err := JSON(&buf, Consolidate(result(), "EUR", 730, now)); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Consolidation struct {
			Removed    []string `json:"removed"`
			LowerBound int      `json:"lowerBound"`
			Savings    []struct {
				Node     string `json:"node"`
				CO2Known bool   `json:"co2Known"`
			} `json:"savings"`
			Hours float64 `json:"hours"`
		} `json:"consolidation"`
	}
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	c := got.Consolidation
	if len(c.Removed) != 2 || c.LowerBound != 3 || len(c.Savings) != 2 || c.Hours != 730 || c.Savings[1].CO2Known {
		t.Errorf("consolidation = %+v, want 2 removed, bound 3, 2 savings, 730 h, CO₂ of node-b unknown", c)
	}
}

// --- helpers ---

var now = time.Date(2026, 1, 2, 4, 4, 5, 0, time.UTC)

// result is a consolidation of five nodes: node-a and node-b are removed,
// node-a with both savings known, node-b in a region without grid intensity.
func result() consolidation.Result {
	return consolidation.Result{
		Nodes:   []string{"node-a", "node-b", "node-c", "node-d", "node-e"},
		Removed: []string{"node-a", "node-b"},
		Kept: []consolidation.Kept{
			{Node: "node-c", Reason: "replacement default/web-sim0 would stay pending: no node fits"},
			{Node: "node-d", Reason: "the drain fails: PodDisruptionBudget web needs 2 healthy pods and has 2"},
			{Node: "node-e", Reason: "replacement default/web-sim1 would stay pending: no node fits"},
		},
		LowerBound: 3,
		Savings: []estimator.Saving{
			{Node: "node-a", InstanceType: "Standard_D4s_v5", Region: "germanywestcentral",
				MoneyKnown: true, Money: 153.3, CO2Known: true, EnergyKWh: 8.1245496, CO2eKg: 5.0820977648},
			{Node: "node-b", InstanceType: "Standard_D4s_v5",
				MoneyKnown: true, Money: 153.3},
		},
		Info: engine.Info{CreatedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), KubernetesVersion: "v1.36.5"},
	}
}

// render writes the report as text.
func render(t *testing.T, r Report) string {
	t.Helper()
	var b strings.Builder
	if err := Text(&b, r); err != nil {
		t.Fatal(err)
	}
	return b.String()
}
