package loader

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/snapshot"
)

// Load reads a snapshot from a JSON file and validates its format.
func Load(path string) (*snapshot.Snapshot, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading snapshot file %q: %w", path, err)
	}

	var snap snapshot.Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return nil, fmt.Errorf("decoding snapshot JSON: %w", err)
	}

	if snap.FormatVersion != snapshot.FormatVersion {
		return nil, fmt.Errorf(
			"unsupported snapshot format version %q",
			snap.FormatVersion,
		)
	}

	if snap.CreatedAt.IsZero() {
		return nil, fmt.Errorf("invalid snapshot: createdAt is required")
	}

	if snap.KubernetesVersion == "" {
		return nil, fmt.Errorf(
			"invalid snapshot: kubernetesVersion is required",
		)
	}

	return &snap, nil
}
