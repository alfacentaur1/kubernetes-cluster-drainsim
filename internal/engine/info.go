package engine

import (
	"time"

	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/engine/eviction"
	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/engine/state"
	corev1 "k8s.io/api/core/v1"
)

// Info describes the snapshot the simulation runs on, shown at the end of a
// report (Section 5.7).
type Info struct {
	CreatedAt         time.Time
	KubernetesVersion string
	Warnings          []Warning
}

// Warning names pods that use a feature the simulation does not model, so
// the result may differ from a real drain for them (Sections 4.5 and 4.6).
type Warning struct {
	Topic   string   `json:"topic"`
	Message string   `json:"message"`
	Pods    []string `json:"pods"`
}

// Info returns the description of the snapshot. It is computed when the
// engine is created, so it describes the snapshot, not a simulated state.
func (e *Engine) Info() Info { return e.info }

// newInfo finds the pods of the snapshot that a drain would evict and that
// use a feature outside the model. DaemonSet and mirror pods are not evicted,
// so they are not reported.
func newInfo(st *state.ClusterState) Info {
	var volumes, priority, claims []string
	for _, p := range st.BoundPods() {
		if eviction.Decide(p).Action != eviction.Evict {
			continue
		}
		if usesClaim(p) {
			volumes = append(volumes, key(p))
		}
		if p.Spec.Priority != nil && *p.Spec.Priority > 0 {
			priority = append(priority, key(p))
		}
		if len(p.Spec.ResourceClaims) > 0 {
			claims = append(claims, key(p))
		}
	}

	info := Info{CreatedAt: st.CreatedAt(), KubernetesVersion: st.KubernetesVersion()}
	add := func(topic, msg string, pods []string) {
		if len(pods) > 0 {
			info.Warnings = append(info.Warnings, Warning{Topic: topic, Message: msg, Pods: pods})
		}
	}
	add("volume topology",
		"pods use PersistentVolumeClaims; a replacement may be placed on a node where its volume cannot be attached",
		volumes)
	add("priority and preemption",
		"pods have a priority; a real scheduler may evict other pods to place their replacements, the simulation does not",
		priority)
	add("dynamic resource allocation",
		"pods use ResourceClaims; the devices they need are not checked",
		claims)
	return info
}

// usesClaim reports whether the pod mounts a PersistentVolumeClaim.
func usesClaim(p *corev1.Pod) bool {
	for _, v := range p.Spec.Volumes {
		if v.PersistentVolumeClaim != nil {
			return true
		}
	}
	return false
}
