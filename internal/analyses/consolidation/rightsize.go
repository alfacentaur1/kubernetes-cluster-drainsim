package consolidation

import (
	"cmp"
	"math"
	"slices"

	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/engine"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

const (
	// minCPUMillicores and minMemoryBytes are the smallest requests the
	// right-sizing recommends. A container that used nothing measurable
	// still gets a request, because removing the request would change the
	// QoS class of the pod.
	minCPUMillicores = 1
	minMemoryBytes   = 1 << 20 // 1 Mi

	mebibyte = 1 << 20
)

// Recommendation is the right-sized request of one container (FR21, FR22,
// Section 5.5). CPU is in millicores, memory in bytes, as in the usage
// profiles; 0 means no request.
type Recommendation struct {
	Namespace string `json:"namespace"`
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Container string `json:"container"`

	CurrentCPUMillicores     int64 `json:"currentCpuMillicores"`
	CurrentMemoryBytes       int64 `json:"currentMemoryBytes"`
	RecommendedCPUMillicores int64 `json:"recommendedCpuMillicores"`
	RecommendedMemoryBytes   int64 `json:"recommendedMemoryBytes"`

	// Higher is set when a recommended request is above the current one:
	// the container uses more than it reserves (Section 5.5).
	Higher bool `json:"higher,omitempty"`
	// Capped is set when a recommendation was lowered to the limit of the
	// container, because a request may not exceed its limit.
	Capped bool `json:"capped,omitempty"`
	// NoData is set for a container of a profiled workload without usage
	// data; it keeps its requests (Section 5.5).
	NoData bool `json:"noData,omitempty"`
}

// Recommend computes the right-sized requests of every container of the
// workloads in the usage profiles of the snapshot (FR21, FR22): CPU is the
// 95th percentile of usage, memory the maximum plus margin (e.g. 0.15),
// both rounded up (memory to whole Mi) and at least 1m and 1Mi. A request
// is never above the limit of the container. Workloads without a running
// pod are skipped; containers without usage data keep their requests. The
// result is sorted by namespace, kind, name and container (NFR7).
func Recommend(sim engine.Simulation, margin float64) []Recommendation {
	var recs []Recommendation
	for _, p := range sim.UsageProfiles() {
		requests := sim.WorkloadRequests(p.Namespace, p.Kind, p.Name)
		if requests == nil {
			continue // the workload no longer runs
		}
		limits := sim.WorkloadLimits(p.Namespace, p.Kind, p.Name)

		usage := map[string]int{}
		for i, c := range p.Containers {
			usage[c.Name] = i
		}
		for container, req := range requests {
			r := Recommendation{
				Namespace: p.Namespace, Kind: p.Kind, Name: p.Name, Container: container,
				CurrentCPUMillicores: req.Cpu().MilliValue(),
				CurrentMemoryBytes:   req.Memory().Value(),
			}
			i, ok := usage[container]
			if !ok {
				r.NoData = true
				r.RecommendedCPUMillicores = r.CurrentCPUMillicores
				r.RecommendedMemoryBytes = r.CurrentMemoryBytes
				recs = append(recs, r)
				continue
			}
			u := p.Containers[i]
			r.RecommendedCPUMillicores = max(u.CPUP95Millicores, minCPUMillicores)
			r.RecommendedMemoryBytes = max(memoryWithMargin(u.MemoryMaxBytes, margin), minMemoryBytes)

			lim := limits[container]
			if l, ok := lim[corev1.ResourceCPU]; ok && l.MilliValue() < r.RecommendedCPUMillicores {
				r.RecommendedCPUMillicores, r.Capped = l.MilliValue(), true
			}
			if l, ok := lim[corev1.ResourceMemory]; ok && l.Value() < r.RecommendedMemoryBytes {
				r.RecommendedMemoryBytes, r.Capped = l.Value(), true
			}
			r.Higher = r.RecommendedCPUMillicores > r.CurrentCPUMillicores ||
				r.RecommendedMemoryBytes > r.CurrentMemoryBytes
			recs = append(recs, r)
		}
	}
	slices.SortFunc(recs, func(a, b Recommendation) int {
		return cmp.Or(
			cmp.Compare(a.Namespace, b.Namespace),
			cmp.Compare(a.Kind, b.Kind),
			cmp.Compare(a.Name, b.Name),
			cmp.Compare(a.Container, b.Container),
		)
	})
	return recs
}

// memoryWithMargin returns maxBytes · (1 + margin), rounded up to whole Mi.
func memoryWithMargin(maxBytes int64, margin float64) int64 {
	mib := math.Ceil(float64(maxBytes) * (1 + margin) / mebibyte)
	return int64(mib) * mebibyte
}

// Apply sets the recommended requests in the cluster state of the
// simulation (FR23) and returns the number of containers changed, counted
// per pod. Recommendations without usage data change nothing.
func Apply(sim engine.Simulation, recs []Recommendation) int {
	changed := 0
	for _, r := range recs {
		if r.NoData {
			continue
		}
		req := corev1.ResourceList{
			corev1.ResourceCPU:    *resource.NewMilliQuantity(r.RecommendedCPUMillicores, resource.DecimalSI),
			corev1.ResourceMemory: *resource.NewQuantity(r.RecommendedMemoryBytes, resource.BinarySI),
		}
		changed += sim.SetRequests(r.Namespace, r.Kind, r.Name, r.Container, req)
	}
	return changed
}
