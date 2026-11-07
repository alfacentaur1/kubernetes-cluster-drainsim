type Config struct {
PolicyTargets PolicyTargets `json:"policyTargets"`
Estimate      Estimate      `json:"estimate"`
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

// Period returns the period of the estimate in hours.
func (e Estimate) Period() float64 {
if e.Hours == 0 {
return 730 // one month: 8760 h / 12
}
return e.Hours
}