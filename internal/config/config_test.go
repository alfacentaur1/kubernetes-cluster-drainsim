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

// write saves content to a temporary file and returns its path.
func write(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "drainsim.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
