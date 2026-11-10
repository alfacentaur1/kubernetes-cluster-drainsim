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
	if want := "wrote " + output + ": 2 nodes, 3 pods, 0 PDBs"; !strings.Contains(out, want) {
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

// TestSnapshotUsageFlags checks that --prometheus and --usage-window reach
// the collector (FR20) and that invalid values are rejected before the
// cluster is read (exit code 2).
func TestSnapshotUsageFlags(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantOpts   collector.Options
		wantErr    string // empty means success
		wantOutput string
	}{
		{
			name:       "no prometheus",
			args:       nil,
			wantOpts:   collector.Options{},
			wantOutput: "3 pods, 0 PDBs\n",
		},
		{
			name:       "prometheus with the default window",
			args:       []string{"--prometheus", "http://localhost:9090"},
			wantOpts:   collector.Options{PrometheusURL: "http://localhost:9090", UsageWindow: 7 * 24 * time.Hour},
			wantOutput: "0 PDBs, 1 usage profiles\n",
		},
		{
			name:       "prometheus with a window",
			args:       []string{"--prometheus", "https://prom.example:9090/", "--usage-window", "24h"},
			wantOpts:   collector.Options{PrometheusURL: "https://prom.example:9090/", UsageWindow: 24 * time.Hour},
			wantOutput: "1 usage profiles",
		},
		{
			name:    "window without prometheus",
			args:    []string{"--usage-window", "1d"},
			wantErr: "--usage-window requires --prometheus",
		},
		{
			name:    "invalid window",
			args:    []string{"--prometheus", "http://localhost:9090", "--usage-window", "1w"},
			wantErr: "invalid --usage-window",
		},
		{
			name:    "URL without scheme",
			args:    []string{"--prometheus", "localhost:9090"},
			wantErr: "invalid --prometheus",
		},
		{
			name:    "URL with another scheme",
			args:    []string{"--prometheus", "ftp://localhost"},
			wantErr: "invalid --prometheus",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got *collector.Options
			newCollector := func(opts collector.Options) (Collector, error) {
				got = &opts
				snap := validSnapshot()
				if opts.PrometheusURL != "" {
					snap.UsageProfiles = []snapshot.UsageProfile{{Namespace: "shop", Kind: "Deployment", Name: "web"}}
				}
				return fakeCollector{snap: snap}, nil
			}

			output := filepath.Join(t.TempDir(), "snapshot.json")
			var out bytes.Buffer
			cmd := NewRootCommand(Deps{NewCollector: newCollector})
			cmd.SetOut(&out)
			cmd.SetErr(&bytes.Buffer{})
			cmd.SetArgs(append([]string{"snapshot", "-o", output}, tt.args...))
			err := cmd.ExecuteContext(context.Background())

			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want substring %q", err, tt.wantErr)
				}
				if got != nil {
					t.Errorf("collector was created despite invalid flags")
				}
				return
			}
			if err != nil {
				t.Fatalf("snapshot command error = %v", err)
			}
			if got == nil || *got != tt.wantOpts {
				t.Errorf("collector options = %+v, want %+v", got, tt.wantOpts)
			}
			if !strings.Contains(out.String(), tt.wantOutput) {
				t.Errorf("output = %q, want substring %q", out.String(), tt.wantOutput)
			}
		})
	}
}
