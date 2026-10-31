package loader

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/snapshot"
)

func TestLoadValidSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "snapshot.json")
	data := `{
		"formatVersion": "drainsim.snapshot/v1",
		"createdAt": "2026-01-02T03:04:05Z",
		"kubernetesVersion": "v1.33.1",
		"nodes": [],
		"pods": [],
		"namespaces": [],
		"pdbs": [],
		"replicaSets": [],
		"deployments": [],
		"statefulSets": [],
		"daemonSets": [],
		"jobs": []
	}`

	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if got.FormatVersion != snapshot.FormatVersion {
		t.Errorf("unexpected format version: %q", got.FormatVersion)
	}
	if got.KubernetesVersion != "v1.33.1" {
		t.Errorf("unexpected Kubernetes version: %q", got.KubernetesVersion)
	}
	if !got.CreatedAt.Equal(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)) {
		t.Errorf("unexpected creation time: %v", got.CreatedAt)
	}
}

func TestLoadRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name string
		data string
		want string
	}{
		{
			name: "invalid JSON",
			data: `{`,
			want: "decoding snapshot JSON",
		},
		{
			name: "unsupported version",
			data: `{"formatVersion":"unknown/v99"}`,
			want: "unsupported snapshot format version",
		},
		{
			name: "missing creation time",
			data: `{"formatVersion":"drainsim.snapshot/v1","kubernetesVersion":"v1.33.1"}`,
			want: "createdAt is required",
		},
		{
			name: "missing Kubernetes version",
			data: `{"formatVersion":"drainsim.snapshot/v1","createdAt":"2026-01-02T03:04:05Z"}`,
			want: "kubernetesVersion is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "snapshot.json")
			if err := os.WriteFile(path, []byte(tt.data), 0600); err != nil {
				t.Fatal(err)
			}

			_, err := Load(path)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Load() error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "missing.json"))
	if err == nil {
		t.Fatal("Load() expected an error for a missing file")
	}
}
