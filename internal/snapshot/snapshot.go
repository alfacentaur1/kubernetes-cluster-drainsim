// Package snapshot defines the snapshot artifact: the state of a cluster at
// one moment, stored as versioned JSON (Section 5.2).
package snapshot

import (
	"errors"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
)

// FormatVersion identifies the version of the snapshot format. The loader
// rejects snapshots with an unknown version (FR4, NFR8).
const FormatVersion = "drainsim.snapshot/v1"

var (
	// ErrUnsupportedFormatVersion is returned for a snapshot whose
	// FormatVersion is not FormatVersion.
	ErrUnsupportedFormatVersion = errors.New("unsupported snapshot format version")
	// ErrInvalidSnapshot is returned for a snapshot that is missing required data.
	ErrInvalidSnapshot = errors.New("invalid snapshot")
)

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

	// UsageProfiles is the measured usage of the workloads (Figure 5.2,
	// FR20). It is optional: a snapshot taken without Prometheus has none,
	// and the analyses then work with the requests only (Section 5.5).
	UsageProfiles []UsageProfile `json:"usageProfiles,omitempty"`
}

// UsageProfile is the measured resource usage of one workload over a window
// (entity Usage profile, Figure 5.2, FR20). The collector reads it from
// Prometheus; the right-sizing uses it to replace the requests of the
// containers (FR21, FR22).
type UsageProfile struct {
	// Namespace, Kind and Name identify the workload as an operator knows
	// it: a Deployment, StatefulSet, DaemonSet or Job, or the ReplicaSet or
	// Pod itself when nothing owns it.
	Namespace string `json:"namespace"`
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	// Window is the observed period, e.g. "168h".
	Window string `json:"window"`
	// Containers holds the usage per container, sorted by name.
	Containers []ContainerUsage `json:"containers"`
}

// ContainerUsage is the usage of one container of a workload: the 95th
// percentile of CPU (FR21) and the maximum of memory (FR22) over the window,
// both the maximum over all pods of the workload.
type ContainerUsage struct {
	Name             string `json:"name"`
	CPUP95Millicores int64  `json:"cpuP95Millicores"`
	MemoryMaxBytes   int64  `json:"memoryMaxBytes"`
}
