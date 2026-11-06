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
// state of the cluster (Section 5.2). Prices, carbon intensity and energy
// coefficients are added with the consolidation.
type Config struct {
	PolicyTargets PolicyTargets `json:"policyTargets"`
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
	return &c, nil
}
