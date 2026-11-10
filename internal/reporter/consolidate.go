package reporter

import (
	"fmt"
	"time"

	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/analyses/consolidation"
	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/estimator"
)

// Consolidation is the part of a report that only the consolidate command
// has (Section 5.5, Section 5.7, UC10–UC12).
type Consolidation struct {
	// Removed are the nodes that can be removed while all pods still run,
	// in the order they were removed (FR24, FR25).
	Removed []string `json:"removed"`
	// Kept are the nodes that cannot be removed, with the reason.
	Kept []consolidation.Kept `json:"kept"`
	// LowerBound is the smallest number of nodes any solution needs.
	LowerBound int `json:"lowerBound"`
	// Savings holds one estimate per removed node; empty without a cost
	// model (FR26–FR28).
	Savings []estimator.Saving `json:"savings,omitempty"`
	// Total sums the savings that are known; nil without a cost model.
	Total *Total `json:"total,omitempty"`
	// Currency of the money values, only shown (e.g. "EUR").
	Currency string `json:"currency,omitempty"`
	// Hours is the period of the estimate (730 h = one month, FR27).
	Hours float64 `json:"hours,omitempty"`
	// RightSizing is present with --right-size. The fields above then
	// describe the consolidation with the right-sized requests (FR23).
	RightSizing *RightSizing `json:"rightSizing,omitempty"`
}

// RightSizing is the part of the consolidation report about right-sizing
// (FR21–FR23, Section 5.5): the recommended requests and the consolidation
// with the current requests, for comparison.
type RightSizing struct {
	// MemoryMargin is the fraction added to the memory maximum.
	MemoryMargin    float64                        `json:"memoryMargin"`
	Recommendations []consolidation.Recommendation `json:"recommendations"`
	// Current is the consolidation with the requests of the snapshot.
	Current Outcome `json:"current"`
}

// Outcome is a consolidation without its savings: what can be removed and
// how many nodes are needed at least.
type Outcome struct {
	Removed    []string             `json:"removed"`
	Kept       []consolidation.Kept `json:"kept"`
	LowerBound int                  `json:"lowerBound"`
}

// Total is the sum of the known savings of the removed nodes. Nodes whose
// saving is unknown are counted, so the reader sees how much is missing.
type Total struct {
	Money        float64 `json:"money"`
	MoneyUnknown int     `json:"moneyUnknown"` // removed nodes without a known price
	EnergyKWh    float64 `json:"energyKWh"`
	CO2eKg       float64 `json:"co2eKg"`
	CO2Unknown   int     `json:"co2Unknown"` // removed nodes without a known CO₂ saving
}

// Consolidate builds the report of the consolidate command. currency and
// hours come from the estimate section of the configuration; hours is 0
// when no cost model was used, and the savings are then unknown. The report
// always passes: the consolidation is an estimate, not a check, so its exit
// code is 0 (Section 5.7).
func Consolidate(res consolidation.Result, currency string, hours float64, now time.Time) Report {
	c := &Consolidation{
		Removed:    nonNil(res.Removed),
		Kept:       nonNil(res.Kept),
		LowerBound: res.LowerBound,
		Savings:    res.Savings,
		Currency:   currency,
		Hours:      hours,
	}
	if hours > 0 {
		c.Total = total(res.Savings)
	}
	return Report{
		FormatVersion: FormatVersion,
		Command:       "consolidate",
		Passed:        true,
		Summary:       consolidateSummary(len(res.Nodes), c),
		Order:         res.Nodes,
		Drained:       nonNil(res.Removed),
		Steps:         nonNil(steps(res.Events)),
		Snapshot:      snapshotInfo(res.Info, now),
		Nodes:         res.Nodes,
		Consolidation: c,
	}
}

// total sums the known savings and counts the unknown ones.
func total(savings []estimator.Saving) *Total {
	t := &Total{}
	for _, s := range savings {
		if s.MoneyKnown {
			t.Money += s.Money
		} else {
			t.MoneyUnknown++
		}
		if s.CO2Known {
			t.EnergyKWh += s.EnergyKWh
			t.CO2eKg += s.CO2eKg
		} else {
			t.CO2Unknown++
		}
	}
	return t
}

// consolidateSummary says how many nodes can be removed, how many are needed
// at least, and what the removal saves (Section 5.7), e.g. "2 of 5 nodes can
// be removed while all pods still run; at least 3 nodes are needed. Saving
// per month: 306.60 EUR, 10.2 kg CO₂e (1 node unknown)."
func consolidateSummary(nodes int, c *Consolidation) string {
	s := fmt.Sprintf("%d of %d nodes can be removed while all pods still run; at least %s needed.",
		len(c.Removed), nodes, plural(c.LowerBound, "node is", "nodes are"))
	return s + savingsSentence(c)
}

// ConsolidateRightSized builds the report of "consolidate --right-size"
// (FR23): before is the consolidation with the requests of the snapshot,
// after the one with the right-sized requests recs. The savings, steps and
// removable nodes of the report are those of after.
func ConsolidateRightSized(before, after consolidation.Result, recs []consolidation.Recommendation,
	margin float64, currency string, hours float64, now time.Time) Report {
	r := Consolidate(after, currency, hours, now)
	c := r.Consolidation
	if recs == nil {
		recs = []consolidation.Recommendation{}
	}
	c.RightSizing = &RightSizing{
		MemoryMargin:    margin,
		Recommendations: recs,
		Current:         Outcome{Removed: nonNil(before.Removed), Kept: nonNil(before.Kept), LowerBound: before.LowerBound},
	}
	r.Summary = rightSizedSummary(len(after.Nodes), len(before.Removed), c)
	return r
}

// rightSizedSummary compares the consolidation with the current and the
// right-sized requests, e.g. "With the current requests 1 of 5 nodes can be
// removed; with right-sized requests 3 (at least 2 nodes are needed).",
// followed by the savings with the right-sized requests.
func rightSizedSummary(nodes, removedBefore int, c *Consolidation) string {
	s := fmt.Sprintf("With the current requests %d of %d nodes can be removed; with right-sized requests %d (at least %s needed).",
		removedBefore, nodes, len(c.Removed), plural(c.LowerBound, "node is", "nodes are"))
	return s + savingsSentence(c)
}

// savingsSentence describes the savings of the removed nodes, starting with
// a space, or returns "" if no node is removed.
func savingsSentence(c *Consolidation) string {
	s := ""
	if len(c.Removed) == 0 {
		return s
	}
	if c.Total == nil {
		return s + " The savings are unknown: the configuration has no estimate."
	}
	t := c.Total
	removed := len(c.Removed)

	money := "money unknown"
	if t.MoneyUnknown < removed {
		money = fmt.Sprintf("%.2f %s", t.Money, c.Currency)
	}
	co2 := "CO₂e unknown"
	if t.CO2Unknown < removed {
		co2 = fmt.Sprintf("%.1f kg CO₂e", t.CO2eKg)
	}
	s += fmt.Sprintf(" Saving %s: %s, %s", period(c.Hours), money, co2)
	if n := partlyUnknown(c.Savings); n > 0 && n < removed {
		s += fmt.Sprintf(" (%s unknown)", plural(n, "node", "nodes"))
	}
	return s + "."
}

// partlyUnknown counts the removed nodes whose money or CO₂ saving is unknown.
func partlyUnknown(savings []estimator.Saving) int {
	n := 0
	for _, s := range savings {
		if !s.MoneyKnown || !s.CO2Known {
			n++
		}
	}
	return n
}

// period describes the period of the estimate: "per month" for 730 h (FR27),
// otherwise the number of hours.
func period(hours float64) string {
	if hours == 730 {
		return "per month"
	}
	return fmt.Sprintf("over %g h", hours)
}

// plural returns "1 node" or "3 nodes" with the given forms.
func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}

// nonNil returns s, or an empty slice if s is nil, so that the JSON report
// has [] instead of null and tools such as jq can iterate over it.
func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}
