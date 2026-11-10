package config

import (
	"fmt"
	"os"

	"sigs.k8s.io/yaml"
)

//format in yaml:
//policyTargets:
//	cluster: baseline        # every namespace not listed below
//	namespaces:
//		shop: restricted
//		payments: restricted

// Config is the configuration file written by the user (artifact
// Configuration, Figure 5.1). It holds decisions of the user rather than the
// state of the cluster (Section 5.2): target Pod Security levels, prices
// and emission factors, and the margin of the right-sizing.
type Config struct {
	PolicyTargets PolicyTargets `json:"policyTargets"`
	Estimate      Estimate      `json:"estimate"`
	RightSizing   RightSizing   `json:"rightSizing"`
}

// DefaultMemoryMargin is the fraction added to the measured memory maximum
// when no margin is configured: 0.15, the default buffer of KRR
// (Section 2.10, FR22).
const DefaultMemoryMargin = 0.15

// RightSizing configures how the requests of containers are recomputed from
// their usage (FR21–FR23, Section 5.5).
type RightSizing struct {
	// MemoryMargin is the fraction added to the memory maximum, e.g. 0.15
	// for 15 %. Nil means DefaultMemoryMargin; 0 is a valid choice (no
	// margin), so the field is a pointer.
	MemoryMargin *float64 `json:"memoryMargin,omitempty"`
}

// Margin returns the memory margin, or DefaultMemoryMargin if none is set.
func (r RightSizing) Margin() float64 {
	if r.MemoryMargin == nil {
		return DefaultMemoryMargin
	}
	return *r.MemoryMargin
}

// PolicyTargets are the target Pod Security levels for the policy impact
// analysis (FR29): one level per namespace, and one for every namespace that
// is not listed. Levels are "privileged", "baseline" or "restricted".
type PolicyTargets struct {
	Cluster    string            `json:"cluster,omitempty"`
	Namespaces map[string]string `json:"namespaces,omitempty"`
}

// Empty reports whether no target level is set.
func (t PolicyTargets) Empty() bool {
	return t.Cluster == "" && len(t.Namespaces) == 0
}

// Load reads a configuration file. Unknown fields are an error, so that a
// typo such as "policyTarget" is not silently ignored (NFR17). The levels
// themselves are checked by the PSA checker that uses them.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading configuration: %w", err)
	}
	var c Config
	if err := yaml.UnmarshalStrict(data, &c); err != nil {
		return nil, fmt.Errorf("configuration %s: %w", path, err)
	}
	if m := c.RightSizing.Margin(); m < 0 {
		return nil, fmt.Errorf("configuration %s: rightSizing.memoryMargin %g must not be negative", path, m)
	}
	return &c, nil
}

// Estimate holds the values the cost and CO₂ estimator needs (FR26–FR28,
// Section 5.5). The user takes them from the price list of the provider and
// from the coefficients of Cloud Carbon Footprint; the tool downloads
// nothing (NFR3, NFR4).
type Estimate struct {
	// Hours is the period of the estimate; 0 means one month (730 h, FR27).
	Hours float64 `json:"hours,omitempty"`
	// Currency is only shown in the report, e.g. "EUR".
	Currency string `json:"currency,omitempty"`
	// Coefficients are the energy coefficients of the provider.
	Coefficients Coefficients `json:"coefficients"`
	// InstanceTypes by the value of the label node.kubernetes.io/instance-type.
	InstanceTypes map[string]InstanceType `json:"instanceTypes,omitempty"`
	// Regions by the value of the label topology.kubernetes.io/region.
	Regions map[string]Region `json:"regions,omitempty"`
}

// Coefficients are the energy coefficients of Cloud Carbon Footprint.
type Coefficients struct {
	MinWattsPerVCPU    float64 `json:"minWattsPerVcpu"`    // idle power of one vCPU, W
	MemoryKWhPerGBHour float64 `json:"memoryKWhPerGBHour"` // energy of 1 GB of memory per hour
	PUE                float64 `json:"pue"`                // power usage effectiveness of the data centre
}

// InstanceType describes one kind of node.
type InstanceType struct {
	PricePerHour         float64 `json:"pricePerHour"`         // in Currency
	EmbodiedGramsPerHour float64 `json:"embodiedGramsPerHour"` // embodied emissions, g CO₂e per hour
}

// Region describes where a node runs.
type Region struct {
	GridGramsPerKWh float64 `json:"gridGramsPerKWh"` // grid intensity, g CO₂e per kWh
}

// Empty reports whether the estimate section configures no instance type.
// Without instance types no saving can be known, so the consolidation runs
// without a cost model (Section 5.5).
func (e Estimate) Empty() bool {
	return len(e.InstanceTypes) == 0
}

// Period returns the period of the estimate in hours.
func (e Estimate) Period() float64 {
	if e.Hours == 0 {
		return 730 // one month: 8760 h / 12
	}
	return e.Hours
}
