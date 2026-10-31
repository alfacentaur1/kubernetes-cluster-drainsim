package snapshot

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
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
