// Package snapshot defines the snapshot artifact: the state of a cluster at
// one moment, stored as versioned JSON (Section 5.2).
package snapshot

import (
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
)

// FormatVersion identifies the version of the snapshot format. The loader
// rejects snapshots with an unknown version (FR4, NFR8).
const FormatVersion = "drainsim.snapshot/v1"

// Snapshot is a copy of the cluster objects that the simulation needs.
// The objects are kept in the form returned by the Kubernetes API, so that
// they can be passed directly to the scheduler framework and to the
// Pod Security Admission library.
type Snapshot struct {
	// FormatVersion is the version of this format, see FormatVersion.
	FormatVersion string `json:"formatVersion"`
	// CreatedAt is the time when the snapshot was taken.
	CreatedAt time.Time `json:"createdAt"`
	// KubernetesVersion is the version of the API server, e.g. "v1.33.1".
	KubernetesVersion string `json:"kubernetesVersion"`

	Nodes        []corev1.Node                  `json:"nodes"`
	Pods         []corev1.Pod                   `json:"pods"`
	Namespaces   []corev1.Namespace             `json:"namespaces"`
	PDBs         []policyv1.PodDisruptionBudget `json:"pdbs"`
	ReplicaSets  []appsv1.ReplicaSet            `json:"replicaSets"`
	Deployments  []appsv1.Deployment            `json:"deployments"`
	StatefulSets []appsv1.StatefulSet           `json:"statefulSets"`
	DaemonSets   []appsv1.DaemonSet             `json:"daemonSets"`
	Jobs         []batchv1.Job                  `json:"jobs"`
}
