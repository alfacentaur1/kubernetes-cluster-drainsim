package estimator

// Saving is what removing one node saves over the period of the estimate
// (component Cost and CO₂ estimator, Section 5.5). Money and CO₂ are known
// separately: a node may have a price but no grid intensity (FR27, FR28).
type Saving struct {
	Node         string `json:"node"`
	InstanceType string `json:"instanceType,omitempty"`
	Region       string `json:"region,omitempty"`

	MoneyKnown bool    `json:"moneyKnown"`
	Money      float64 `json:"money"`

	CO2Known  bool    `json:"co2Known"`
	EnergyKWh float64 `json:"energyKWh"`
	CO2eKg    float64 `json:"co2eKg"`
}
