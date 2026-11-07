package estimator

import (
	"math"
	"testing"

	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/config"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// A node with a known instance type and region has both savings, computed
// with the formulas of Section 5.5 over one month (FR27, FR28).
func TestEstimateKnownTypeAndRegion(t *testing.T) {
	s := New(cfg()).Estimate(node("Standard_D4s_v5", "germanywestcentral"))

	// Computed by hand: 4 vCPU, 16 GB, 730 h, CCF Azure coefficients.
	wantMoney := 0.21 * 730                              // 153.3
	wantKWh := (4*0.78/1000 + 16*0.000392) * 730 * 1.185 // 8.1245496
	wantKg := (wantKWh*338 + 3.2*730) / 1000             // 5.0820977648
	if !s.MoneyKnown || !near(s.Money, wantMoney) || !near(s.Money, 153.3) {
		t.Errorf("Money = %v (known %v), want %v", s.Money, s.MoneyKnown, wantMoney)
	}
	if !s.CO2Known || !near(s.EnergyKWh, wantKWh) || !near(s.EnergyKWh, 8.1245496) {
		t.Errorf("EnergyKWh = %v (known %v), want %v", s.EnergyKWh, s.CO2Known, wantKWh)
	}
	if !near(s.CO2eKg, wantKg) || !near(s.CO2eKg, 5.0820977648) {
		t.Errorf("CO2eKg = %v, want %v", s.CO2eKg, wantKg)
	}
	if s.Node != "node-a" || s.InstanceType != "Standard_D4s_v5" || s.Region != "germanywestcentral" {
		t.Errorf("Saving = %+v, want node-a, its instance type and region", s)
	}
}

// An instance type missing from the configuration makes both savings
// unknown; the node is still removed by the consolidation.
func TestEstimateUnknownType(t *testing.T) {
	s := New(cfg()).Estimate(node("Standard_E8s_v5", "germanywestcentral"))

	if s.MoneyKnown || s.CO2Known || s.Money != 0 || s.CO2eKg != 0 {
		t.Errorf("Saving = %+v, want both savings unknown", s)
	}
	if s.Node != "node-a" {
		t.Errorf("Node = %q, want node-a", s.Node)
	}
}

// A known instance type in an unknown region has a price but no grid
// intensity: money is known, CO₂ is not.
func TestEstimateUnknownRegion(t *testing.T) {
	s := New(cfg()).Estimate(node("Standard_D4s_v5", "westeurope"))

	if !s.MoneyKnown || !near(s.Money, 153.3) {
		t.Errorf("Money = %v (known %v), want 153.3", s.Money, s.MoneyKnown)
	}
	if s.CO2Known || s.EnergyKWh != 0 || s.CO2eKg != 0 {
		t.Errorf("Saving = %+v, want CO₂ unknown", s)
	}
}

// --- helpers ---

// cfg returns the example configuration of the specification; Hours is 0,
// so the period is one month.
func cfg() config.Estimate {
	return config.Estimate{
		Currency: "EUR",
		Coefficients: config.Coefficients{
			MinWattsPerVCPU:    0.78,
			MemoryKWhPerGBHour: 0.000392,
			PUE:                1.185,
		},
		InstanceTypes: map[string]config.InstanceType{
			"Standard_D4s_v5": {PricePerHour: 0.21, EmbodiedGramsPerHour: 3.2},
		},
		Regions: map[string]config.Region{
			"germanywestcentral": {GridGramsPerKWh: 338},
		},
	}
}

// node returns node-a with 4 vCPU and 16 GB of capacity. Allocatable is
// smaller, so a test would notice if it were used instead of Capacity.
func node(instanceType, region string) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "node-a", Labels: map[string]string{
			corev1.LabelInstanceTypeStable: instanceType,
			corev1.LabelTopologyRegion:     region,
		}},
		Status: corev1.NodeStatus{
			Capacity: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("4"),
				corev1.ResourceMemory: resource.MustParse("16G"),
			},
			Allocatable: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("3860m"),
				corev1.ResourceMemory: resource.MustParse("12G"),
			},
		},
	}
}

// near reports whether two floats are equal up to rounding.
func near(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}
