package reporter

import (
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
