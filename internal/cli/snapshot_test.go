package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/collector"
	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/snapshot"
	corev1 "k8s.io/api/core/v1"
)

// fakeCollector returns a fixed snapshot or error.
type fakeCollector struct {
	snap *snapshot.Snapshot
	err  error
}

func (f fakeCollector) Collect(context.Context) (*snapshot.Snapshot, error) {
	return f.snap, f.err
}

// runSnapshotCmd runs "drainsim snapshot -o <output>" with the given collector
// factory and returns the command output and error.
func runSnapshotCmd(t *testing.T, newCollector func(collector.Options) (Collector, error), output string) (string, error) {
	t.Helper()

	var out bytes.Buffer
	cmd := NewRootCommand(Deps{NewCollector: newCollector})
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"snapshot", "-o", output})

	err := cmd.ExecuteContext(context.Background())
	return out.String(), err
}

func validSnapshot() *snapshot.Snapshot {
	return &snapshot.Snapshot{
		FormatVersion:     snapshot.FormatVersion,
		CreatedAt:         time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		KubernetesVersion: "v1.33.1",
		Nodes:             []corev1.Node{{}, {}},
		Pods:              []corev1.Pod{{}, {}, {}},
	}
}

func TestSnapshotWritesFile(t *testing.T) {
	output := filepath.Join(t.TempDir(), "snapshot.json")
	newCollector := func(collector.Options) (Collector, error) {
		return fakeCollector{snap: validSnapshot()}, nil
	}

	out, err := runSnapshotCmd(t, newCollector, output)
	if err != nil {
		t.Fatalf("snapshot command error = %v", err)
	}

	if _, err := os.Stat(output); err != nil {
		t.Errorf("snapshot file was not written: %v", err)
	}
	if want := "collected 2 nodes, 3 pods, 0 PDBs"; !strings.Contains(out, want) {
		t.Errorf("output = %q, want substring %q", out, want)
	}
}

func TestSnapshotReturnsErrorsUnchanged(t *testing.T) {
	errCreate := errors.New("loading kubeconfig: no such file")
	errCollect := errors.New("listing nodes: Unauthorized")

	tests := []struct {
		name         string
		newCollector func(collector.Options) (Collector, error)
		wantErr      error
	}{
		{
			name: "collector creation fails",
			newCollector: func(collector.Options) (Collector, error) {
				return nil, errCreate
			},
			wantErr: errCreate,
		},
		{
			name: "collecting fails",
			newCollector: func(collector.Options) (Collector, error) {
				return fakeCollector{err: errCollect}, nil
			},
			wantErr: errCollect,
		},
		{
			name: "writing fails",
			newCollector: func(collector.Options) (Collector, error) {
				return fakeCollector{snap: &snapshot.Snapshot{FormatVersion: "unknown/v99"}}, nil
			},
			wantErr: snapshot.ErrUnsupportedFormatVersion,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := filepath.Join(t.TempDir(), "snapshot.json")

			_, err := runSnapshotCmd(t, tt.newCollector, output)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("error = %v, want %v", err, tt.wantErr)
			}
			// The command must not add its own prefix to the error.
			if !strings.HasPrefix(err.Error(), tt.wantErr.Error()) {
				t.Errorf("error = %q, want it to start with %q", err, tt.wantErr)
			}

			if _, err := os.Stat(output); !os.IsNotExist(err) {
				t.Errorf("snapshot file should not exist after an error")
			}
		})
	}
}
