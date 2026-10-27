package snapshot

import (
	"time"

	"k8s.io/component-helpers/scheduling/corev1"
)

const FormatVersion = "drainsim.snapshot/v1"

type Snapshot struct {
	FormatVersion     string                         `json:"formatVersion"`
	CreatedAt         time.Time                      `json:"createdAt"`
	KubernetesVersion string                         `json:"kubernetesVersion"`
	Nodes             []corev1.Node                  `json:"nodes"`
	Pods              []corev1.Pod                   `json:"pods"`
	Namespaces        []corev1.Namespace             `json:"namespaces"`
	PDBs              []policyv1.PodDisruptionBudget `json:"pdbs"`
	ReplicaSets       []appsv1.ReplicaSet            `json:"replicaSets"`
	Deployments       []appsv1.Deployment            `json:"deployments"`
	StatefulSets      []appsv1.StatefulSet           `json:"statefulSets"`
	DaemonSets        []appsv1.DaemonSet             `json:"daemonSets"`
	Jobs              []batchv1.Job                  `json:"jobs"`
}
