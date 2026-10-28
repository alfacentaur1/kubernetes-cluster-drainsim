package snapshot

import (
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
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
