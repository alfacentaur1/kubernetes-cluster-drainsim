package snapshot

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestWriteAndReadSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "snapshot.json")

	want := &Snapshot{
		FormatVersion:     FormatVersion,
		CreatedAt:         time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		KubernetesVersion: "v1.33.1",
	}

	if err := Write(path, want); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	var got Snapshot
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if got.FormatVersion != want.FormatVersion {
		t.Errorf("FormatVersion = %q, want %q",
			got.FormatVersion, want.FormatVersion)
	}
	if !got.CreatedAt.Equal(want.CreatedAt) {
		t.Errorf("CreatedAt = %v, want %v",
			got.CreatedAt, want.CreatedAt)
	}
	if got.KubernetesVersion != want.KubernetesVersion {
		t.Errorf("KubernetesVersion = %q, want %q",
			got.KubernetesVersion, want.KubernetesVersion)
	}
}

func TestWriteRejectsNilSnapshot(t *testing.T) {
	err := Write(filepath.Join(t.TempDir(), "snapshot.json"), nil)
	if !errors.Is(err, ErrInvalidSnapshot) {
		t.Fatalf("Write() error = %v, want ErrInvalidSnapshot", err)
	}
}

func TestWriteRejectsUnsupportedVersion(t *testing.T) {
	s := &Snapshot{FormatVersion: "unknown/v99"}

	err := Write(filepath.Join(t.TempDir(), "snapshot.json"), s)
	if !errors.Is(err, ErrUnsupportedFormatVersion) {
		t.Fatalf("Write() error = %v, want ErrUnsupportedFormatVersion", err)
	}
}

// TestWriteAndReadUsageProfiles checks that usage profiles survive a round
// trip and that a snapshot without them has no "usageProfiles" key, so older
// snapshots and new ones without Prometheus look the same (FR20, NFR8).
func TestWriteAndReadUsageProfiles(t *testing.T) {
	profiles := []UsageProfile{{
		Namespace: "shop", Kind: "Deployment", Name: "web", Window: "168h",
		Containers: []ContainerUsage{{Name: "app", CPUP95Millicores: 250, MemoryMaxBytes: 128 << 20}},
	}}

	tests := []struct {
		name     string
		profiles []UsageProfile
	}{
		{name: "with profiles", profiles: profiles},
		{name: "without profiles", profiles: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "snapshot.json")
			in := &Snapshot{
				FormatVersion:     FormatVersion,
				CreatedAt:         time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
				KubernetesVersion: "v1.36.4",
				UsageProfiles:     tt.profiles,
			}
			if err := Write(path, in); err != nil {
				t.Fatalf("Write() error = %v", err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("ReadFile() error = %v", err)
			}

			var raw map[string]json.RawMessage
			if err := json.Unmarshal(data, &raw); err != nil {
				t.Fatalf("invalid JSON: %v", err)
			}
			if _, ok := raw["usageProfiles"]; ok != (tt.profiles != nil) {
				t.Errorf("usageProfiles key present = %v, want %v", ok, tt.profiles != nil)
			}

			var got Snapshot
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("invalid JSON: %v", err)
			}
			if !reflect.DeepEqual(got.UsageProfiles, tt.profiles) {
				t.Errorf("UsageProfiles = %+v, want %+v", got.UsageProfiles, tt.profiles)
			}
		})
	}
}
