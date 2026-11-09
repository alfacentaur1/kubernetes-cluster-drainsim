package collector

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/snapshot"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// DefaultUsageWindow is the default length of the usage history: one week,
// so that daily and weekly peaks are included (FR20).
const DefaultUsageWindow = 7 * 24 * time.Hour

// minUsageWindow is the shortest window that makes sense: the CPU query
// computes rates over 5 minutes.
const minUsageWindow = 5 * time.Minute

// ErrInvalidWindow is returned by ParseWindow for a window that is not a
// valid duration or is too short.
var ErrInvalidWindow = errors.New("invalid usage window")

// ParseWindow parses the length of the usage history, e.g. "7d", "168h" or
// "90m". Besides the units of time.ParseDuration it accepts whole days with
// the suffix "d", as Prometheus does. The window must be at least 5 minutes.
func ParseWindow(s string) (time.Duration, error) {
	var d time.Duration
	if days, ok := strings.CutSuffix(s, "d"); ok {
		n, err := strconv.Atoi(days)
		if err != nil {
			return 0, fmt.Errorf("%w %q: %w", ErrInvalidWindow, s, err)
		}
		d = time.Duration(n) * 24 * time.Hour
	} else {
		var err error
		if d, err = time.ParseDuration(s); err != nil {
			return 0, fmt.Errorf("%w %q: %w", ErrInvalidWindow, s, err)
		}
	}
	if d < minUsageWindow {
		return 0, fmt.Errorf("%w %q: must be at least %s", ErrInvalidWindow, s, minUsageWindow)
	}
	return d, nil
}

// formatWindow writes a window in the duration syntax of Prometheus with
// the largest whole unit, e.g. "168h" for seven days. The same text is
// stored in UsageProfile.Window.
func formatWindow(d time.Duration) string {
	switch {
	case d%time.Hour == 0:
		return fmt.Sprintf("%dh", d/time.Hour)
	case d%time.Minute == 0:
		return fmt.Sprintf("%dm", d/time.Minute)
	default:
		return fmt.Sprintf("%ds", d/time.Second)
	}
}

// ownerJoin is the right-hand side of both usage queries: the owner of every
// pod that existed during the window. max_over_time keeps pods that were
// replaced during the window. Only the controller is used as the owner, as
// in metav1.GetControllerOf; a pod without an owner has owner_is_controller
// "<none>".
const ownerJoin = `* on(namespace, pod) group_left(owner_kind, owner_name)
  max by (namespace, pod, owner_kind, owner_name) (max_over_time(kube_pod_owner{owner_is_controller!="false"}[%[1]s]))`

// cpuQuery returns the 95th percentile of the CPU usage (in cores) of every
// container over the window (FR21), with the owner of its pod.
func cpuQuery(window string) string {
	return fmt.Sprintf(`max by (namespace, pod, owner_kind, owner_name, container) (
  quantile_over_time(0.95, rate(container_cpu_usage_seconds_total{container!="",image!=""}[5m])[%[1]s:1m])
  `+ownerJoin+`)`, window)
}

// memoryQuery returns the maximum working set (in bytes) of every container
// over the window (FR22), with the owner of its pod.
func memoryQuery(window string) string {
	return fmt.Sprintf(`max by (namespace, pod, owner_kind, owner_name, container) (
  max_over_time(container_memory_working_set_bytes{container!="",image!=""}[%[1]s])
  `+ownerJoin+`)`, window)
}

// workloadKey identifies a workload (Namespace, Kind, Name).
type workloadKey struct{ namespace, kind, name string }

// collectUsage reads the usage of all containers from Prometheus and
// assigns it to workloads (FR20). A ReplicaSet owned by a Deployment is
// resolved to the Deployment with the ReplicaSets of the snapshot, by the
// rule of state.Workload; a pod without an owner is its own workload of kind
// "Pod". A workload gets the maximum over its pods, which is conservative.
// Samples that are not finite numbers are skipped. The profiles are sorted
// by namespace, kind and name, the containers by name (NFR7).
func collectUsage(ctx context.Context, c *promClient, window time.Duration, at time.Time,
	replicaSets []appsv1.ReplicaSet) ([]snapshot.UsageProfile, error) {
	w := formatWindow(window)

	cpu, err := c.query(ctx, cpuQuery(w), at)
	if err != nil {
		return nil, fmt.Errorf("reading CPU usage: %w", err)
	}
	memory, err := c.query(ctx, memoryQuery(w), at)
	if err != nil {
		return nil, fmt.Errorf("reading memory usage: %w", err)
	}

	// Controller of each ReplicaSet, by "namespace/name".
	rsOwner := map[string]*metav1.OwnerReference{}
	for i := range replicaSets {
		rs := &replicaSets[i]
		if ref := metav1.GetControllerOf(rs); ref != nil {
			rsOwner[rs.Namespace+"/"+rs.Name] = ref
		}
	}
	workload := func(l map[string]string) workloadKey {
		ns, kind, name := l["namespace"], l["owner_kind"], l["owner_name"]
		switch {
		case kind == "" || kind == "<none>":
			return workloadKey{ns, "Pod", l["pod"]}
		case kind == "ReplicaSet":
			if ref, ok := rsOwner[ns+"/"+name]; ok {
				return workloadKey{ns, ref.Kind, ref.Name} // usually a Deployment
			}
		}
		return workloadKey{ns, kind, name}
	}

	usage := map[workloadKey]map[string]*snapshot.ContainerUsage{}
	add := func(samples []promSample, set func(u *snapshot.ContainerUsage, v int64)) {
		for _, s := range samples {
			if math.IsNaN(s.Value) || math.IsInf(s.Value, 0) || s.Labels["container"] == "" {
				continue
			}
			key := workload(s.Labels)
			if usage[key] == nil {
				usage[key] = map[string]*snapshot.ContainerUsage{}
			}
			name := s.Labels["container"]
			u := usage[key][name]
			if u == nil {
				u = &snapshot.ContainerUsage{Name: name}
				usage[key][name] = u
			}
			set(u, int64(math.Ceil(s.Value))) // round up, never below the measured usage
		}
	}
	// CPU comes in cores; convert to millicores before rounding.
	for i := range cpu {
		cpu[i].Value *= 1000
	}
	add(cpu, func(u *snapshot.ContainerUsage, v int64) {
		u.CPUP95Millicores = max(u.CPUP95Millicores, v)
	})
	add(memory, func(u *snapshot.ContainerUsage, v int64) {
		u.MemoryMaxBytes = max(u.MemoryMaxBytes, v)
	})

	profiles := make([]snapshot.UsageProfile, 0, len(usage))
	for key, containers := range usage {
		p := snapshot.UsageProfile{Namespace: key.namespace, Kind: key.kind, Name: key.name, Window: w}
		for _, u := range containers {
			p.Containers = append(p.Containers, *u)
		}
		slices.SortFunc(p.Containers, func(a, b snapshot.ContainerUsage) int { return cmp.Compare(a.Name, b.Name) })
		profiles = append(profiles, p)
	}
	slices.SortFunc(profiles, func(a, b snapshot.UsageProfile) int {
		return cmp.Or(
			cmp.Compare(a.Namespace, b.Namespace),
			cmp.Compare(a.Kind, b.Kind),
			cmp.Compare(a.Name, b.Name),
		)
	})
	return profiles, nil
}
