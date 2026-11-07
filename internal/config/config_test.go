package config

import (
	"os"
	"path/filepath"
	"testing"
)

// A valid file gives the target levels (FR29).
func TestLoad(t *testing.T) {
	path := write(t, "policyTargets:\n  cluster: baseline\n  namespaces:\n    shop: restricted\n")

	c, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if c.PolicyTargets.Cluster != "baseline" || c.PolicyTargets.Namespaces["shop"] != "restricted" {
		t.Errorf("Load() = %+v, want cluster baseline and shop restricted", c.PolicyTargets)
	}
}

// A typo in a key is an error, not a silently empty configuration (NFR17).
func TestLoadRejectsUnknownField(t *testing.T) {
	path := write(t, "policyTarget:\n  cluster: baseline\n")

	if _, err := Load(path); err == nil {
		t.Fatal("Load() error = nil, want an error for the unknown field policyTarget")
	}
}

// The estimate section of the specification parses with strict parsing
// (FR26-FR28).
func TestLoadEstimate(t *testing.T) {
	path := write(t, `estimate:
  hours: 730
  currency: EUR
  coefficients:
    minWattsPerVcpu: 0.78
    memoryKWhPerGBHour: 0.000392
    pue: 1.185
  instanceTypes:
    Standard_D4s_v5:
      pricePerHour: 0.21
      embodiedGramsPerHour: 3.2
  regions:
    germanywestcentral:
      gridGramsPerKWh: 338
`)

	c, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	e := c.Estimate
	if e.Hours != 730 || e.Currency != "EUR" {
		t.Errorf("Hours, Currency = %v, %q, want 730, EUR", e.Hours, e.Currency)
	}
	want := Coefficients{MinWattsPerVCPU: 0.78, MemoryKWhPerGBHour: 0.000392, PUE: 1.185}
	if e.Coefficients != want {
		t.Errorf("Coefficients = %+v, want %+v", e.Coefficients, want)
	}
	if it := e.InstanceTypes["Standard_D4s_v5"]; it.PricePerHour != 0.21 || it.EmbodiedGramsPerHour != 3.2 {
		t.Errorf("InstanceTypes = %+v, want Standard_D4s_v5 at 0.21/h and 3.2 g/h", e.InstanceTypes)
	}
	if r := e.Regions["germanywestcentral"]; r.GridGramsPerKWh != 338 {
		t.Errorf("Regions = %+v, want germanywestcentral at 338 g/kWh", e.Regions)
	}
}

// Without hours the period is one month, 730 h (FR27).
func TestPeriodDefaultsToMonth(t *testing.T) {
	path := write(t, "estimate:\n  currency: EUR\n")

	c, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got := c.Estimate.Period(); got != 730 {
		t.Errorf("Period() = %v, want 730", got)
	}
	if got := (Estimate{Hours: 24}).Period(); got != 24 {
		t.Errorf("Period() with hours 24 = %v, want 24", got)
	}
}

// An estimate without instance types is empty: no saving can be known.
func TestEstimateEmpty(t *testing.T) {
	if !(Estimate{Currency: "EUR"}).Empty() {
		t.Error("Empty() = false for an estimate without instance types, want true")
	}
	e := Estimate{InstanceTypes: map[string]InstanceType{"Standard_D4s_v5": {PricePerHour: 0.21}}}
	if e.Empty() {
		t.Error("Empty() = true for an estimate with an instance type, want false")
	}
}

// write saves content to a temporary file and returns its path.
func write(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "drainsim.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// The example configuration in examples/ stays valid: it parses with strict
// parsing and configures both analyses.
func TestExampleConfiguration(t *testing.T) {
	c, err := Load(filepath.Join("..", "..", "examples", "drainsim.yaml"))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if c.PolicyTargets.Empty() || c.Estimate.Empty() {
		t.Errorf("Load() = %+v, want policy targets and an estimate", c)
	}
	if c.Estimate.Period() != 730 {
		t.Errorf("Period() = %v, want 730", c.Estimate.Period())
	}
	if _, ok := c.Estimate.Regions["westeurope"]; !ok {
		t.Errorf("Regions = %v, want westeurope", c.Estimate.Regions)
	}
}
