package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/reporter"
	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/snapshot"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// Without --config the removable nodes are found and the savings are
// unknown (Section 5.5). The run exits with 0.
func TestConsolidateWithoutConfig(t *testing.T) {
	out, err := runConsolidateCmd(t, writeConsolidateSnapshot(t))

	if code := reporter.ExitCode(err); code != reporter.ExitOK {
		t.Fatalf("exit code = %d (err %v), want 0", code, err)
	}
	for _, want := range []string{
		"PASSED  1 of 3 nodes can be removed while all pods still run; at least 2 nodes are needed.",
		"The savings are unknown",
		"Removable nodes, in order: node-a\n",
		"node-b: replacement default/app-large-b-sim",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output =\n%s\nwant substring %q", out, want)
		}
	}
}

// With an estimate section, the saving of the removed node is estimated
// (FR26–FR28).
func TestConsolidateWithConfig(t *testing.T) {
	cfg := writeFile(t, "drainsim.yaml", `estimate:
  currency: EUR
  coefficients:
    minWattsPerVcpu: 0.74
    memoryKWhPerGBHour: 0.000392
    pue: 1.185
  instanceTypes:
    Standard_D2s_v5:
      pricePerHour: 0.1032
  regions:
    germanywestcentral:
      gridGramsPerKWh: 338.66
`)

	out, err := runConsolidateCmd(t, writeConsolidateSnapshot(t), "--config", cfg)

	if code := reporter.ExitCode(err); code != reporter.ExitOK {
		t.Fatalf("exit code = %d (err %v), want 0", code, err)
	}
	// 0.1032 EUR/h * 730 h = 75.336 EUR.
	for _, want := range []string{
		"Saving per month: 75.34 EUR,",
		"  node-a (Standard_D2s_v5, germanywestcentral): 75.34 EUR,",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output =\n%s\nwant substring %q", out, want)
		}
	}
}

// A configuration with only policy targets has no estimate: the savings are
// unknown, the command does not fail.
func TestConsolidateConfigWithoutEstimate(t *testing.T) {
	cfg := writeFile(t, "drainsim.yaml", "policyTargets:\n  cluster: baseline\n")

	out, err := runConsolidateCmd(t, writeConsolidateSnapshot(t), "--config", cfg, "--format", "json")

	if code := reporter.ExitCode(err); code != reporter.ExitOK {
		t.Fatalf("exit code = %d (err %v), want 0", code, err)
	}
	if !strings.Contains(out, `"command": "consolidate"`) || strings.Contains(out, `"total"`) {
		t.Errorf("output =\n%s\nwant a consolidate report without a total", out)
	}
}

// Invalid input is exit code 2.
func TestConsolidateInvalidInput(t *testing.T) {
	snap := writeConsolidateSnapshot(t)
	for name, args := range map[string][]string{
		"format":         {"--format", "xml"},
		"unknown node":   {"--nodes", "node-x"},
		"missing config": {"--config", filepath.Join(t.TempDir(), "missing.yaml")},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := runConsolidateCmd(t, snap, args...)
			if code := reporter.ExitCode(err); code != reporter.ExitError {
				t.Errorf("exit code = %d (err %v), want 2", code, err)
			}
		})
	}
}

// --- helpers ---

// runConsolidateCmd runs "drainsim consolidate SNAPSHOT args..." and returns
// its output and error.
func runConsolidateCmd(t *testing.T, snap string, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	cmd := NewRootCommand(Deps{Now: func() time.Time { return time.Date(2026, 1, 2, 4, 4, 5, 0, time.UTC) }})
	cmd.SetOut(&out)
	cmd.SetArgs(append([]string{"consolidate", snap}, args...))
	err := cmd.ExecuteContext(context.Background())
	return out.String(), err
}

// writeConsolidateSnapshot writes three nodes of 2 CPU with 500m, 1200m and
// 1200m requested: only node-a can be removed. The functions in change can
// add to the snapshot before it is written.
func writeConsolidateSnapshot(t *testing.T, change ...func(*snapshot.Snapshot)) string {
	t.Helper()
	snap := &snapshot.Snapshot{
		FormatVersion:     snapshot.FormatVersion,
		CreatedAt:         time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		KubernetesVersion: "v1.36.4",
		Nodes:             []corev1.Node{testNode("node-a"), testNode("node-b"), testNode("node-c")},
		Pods: []corev1.Pod{
			testPod("node-a", "small", "500m"),
			testPod("node-b", "large-b", "1200m"),
			testPod("node-c", "large-c", "1200m"),
		},
	}
	for _, c := range change {
		c(snap)
	}
	path := filepath.Join(t.TempDir(), "snapshot.json")
	if err := snapshot.Write(path, snap); err != nil {
		t.Fatal(err)
	}
	return path
}

// writeFile saves content to a temporary file and returns its path.
func writeFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// testNode returns a Standard_D2s_v5 node in germanywestcentral.
func testNode(name string) corev1.Node {
	res := corev1.ResourceList{
		corev1.ResourceCPU:    resource.MustParse("2"),
		corev1.ResourceMemory: resource.MustParse("8G"),
		corev1.ResourcePods:   resource.MustParse("110"),
	}
	return corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: name, UID: types.UID("node-" + name), Labels: map[string]string{
			corev1.LabelHostname:           name,
			corev1.LabelInstanceTypeStable: "Standard_D2s_v5",
			corev1.LabelTopologyRegion:     "germanywestcentral",
		}},
		Status: corev1.NodeStatus{Capacity: res, Allocatable: res},
	}
}

// testPod returns a Ready pod of the ReplicaSet app-<rs> on the node.
func testPod(nodeName, rs, cpu string) corev1.Pod {
	owner := "app-" + rs
	name := owner + "-aaaaa"
	isController := true
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default", Name: name, GenerateName: owner + "-", UID: types.UID("pod-" + name),
			Labels: map[string]string{"app": rs},
			OwnerReferences: []metav1.OwnerReference{{APIVersion: "apps/v1", Kind: "ReplicaSet", Name: owner,
				UID: types.UID("rs-" + owner), Controller: &isController}},
		},
		Spec: corev1.PodSpec{
			NodeName: nodeName,
			Containers: []corev1.Container{{Name: "app", Image: "registry.k8s.io/pause:3.10",
				Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse(cpu),
					corev1.ResourceMemory: resource.MustParse("64Mi"),
				}}}},
		},
		Status: corev1.PodStatus{
			Phase:      corev1.PodRunning,
			Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}},
		},
	}
}

// With --right-size the two large pods (1200m requested, 600m used) are
// right-sized and one more node can be removed: node-a and node-b, with
// 500m + 600m + 600m = 1700m left on node-c (FR23, Section 5.5).
func TestConsolidateRightSize(t *testing.T) {
	snap := writeConsolidateSnapshot(t, func(s *snapshot.Snapshot) {
		for _, rs := range []string{"large-b", "large-c"} {
			s.UsageProfiles = append(s.UsageProfiles, snapshot.UsageProfile{
				Namespace: "default", Kind: "ReplicaSet", Name: "app-" + rs, Window: "168h",
				Containers: []snapshot.ContainerUsage{{Name: "app", CPUP95Millicores: 600, MemoryMaxBytes: 40 << 20}},
			})
		}
	})
	cfg := writeFile(t, "drainsim.yaml", "rightSizing:\n  memoryMargin: 0.25\n")

	out, err := runConsolidateCmd(t, snap, "--right-size", "--config", cfg)

	if code := reporter.ExitCode(err); code != reporter.ExitOK {
		t.Fatalf("exit code = %d (err %v), want 0", code, err)
	}
	for _, want := range []string{
		"PASSED  With the current requests 1 of 3 nodes can be removed; with right-sized requests 2 (at least 1 node is needed).",
		// 40 Mi · 1.25 = 50 Mi
		"  default/ReplicaSet app-large-b, container app: CPU 1200m → 600m, memory 64Mi → 50Mi\n",
		"With the current requests:\nRemovable nodes, in order: node-a\nLower bound: at least 2 nodes are needed\n",
		"With right-sized requests:\nRemovable nodes, in order: node-a, node-b\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output =\n%s\nwant substring %q", out, want)
		}
	}
}

// --right-size on a snapshot without usage profiles is an input error
// (exit code 2) that says how to take a snapshot with them.
func TestConsolidateRightSizeWithoutProfiles(t *testing.T) {
	_, err := runConsolidateCmd(t, writeConsolidateSnapshot(t), "--right-size")

	if code := reporter.ExitCode(err); code != reporter.ExitError {
		t.Fatalf("exit code = %d (err %v), want 2", code, err)
	}
	if !strings.Contains(err.Error(), "--prometheus") {
		t.Errorf("error = %q, want a hint to take the snapshot with --prometheus", err)
	}
}
