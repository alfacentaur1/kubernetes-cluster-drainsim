package estimator

import (
	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/config"
	corev1 "k8s.io/api/core/v1"
)

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

// Estimator is the Cost and CO₂ estimator of Section 5.5. It computes the
// saving of a removed node from the prices, grid intensities and energy
// coefficients in the configuration; it downloads nothing (NFR3, NFR4). It
// implements consolidation.CostModel (ICostModel).
type Estimator struct {
	cfg config.Estimate
}

// New returns an Estimator that uses the given configuration.
func New(cfg config.Estimate) *Estimator {
	return &Estimator{cfg: cfg}
}

// Estimate returns what removing the node saves over the period of the
// estimate (FR27, FR28). The instance type and the region are read from the
// well-known node labels. Without a known instance type nothing is known;
// without a known region (or without a PUE) only the money is known.
func (e *Estimator) Estimate(node *corev1.Node) Saving {
	if node == nil {
		return Saving{}
	}
	h := e.cfg.Period()
	s := Saving{
		Node:         node.Name,
		InstanceType: node.Labels[corev1.LabelInstanceTypeStable],
		Region:       node.Labels[corev1.LabelTopologyRegion],
	}

	it, ok := e.cfg.InstanceTypes[s.InstanceType]
	if !ok {
		return s // the node is still removed; its saving is unknown
	}
	s.Money = it.PricePerHour * h
	s.MoneyKnown = true

	region, ok := e.cfg.Regions[s.Region]
	c := e.cfg.Coefficients
	if !ok || c.PUE == 0 {
		return s
	}

	// Capacity, not Allocatable: the whole machine draws power, including
	// the part reserved for the system. GB is 10^9 bytes, as in the
	// coefficients of Cloud Carbon Footprint.
	vcpus := float64(node.Status.Capacity.Cpu().Value())
	gb := float64(node.Status.Capacity.Memory().Value()) / 1e9

	// Only the idle power of the CPUs and the memory are saved: the pods keep
	// running on other nodes, so the energy of their work does not go away
	// (Section 5.5). The embodied emissions of the machine are saved in full.
	s.EnergyKWh = (vcpus*c.MinWattsPerVCPU/1000 + gb*c.MemoryKWhPerGBHour) * h * c.PUE
	grams := s.EnergyKWh*region.GridGramsPerKWh + it.EmbodiedGramsPerHour*h
	s.CO2eKg = grams / 1000
	s.CO2Known = true
	return s
}
