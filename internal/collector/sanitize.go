package collector

import (
	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/snapshot"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Sanitize removes managed fields, last-applied-configuration annotations and
// all literal env values from a snapshot, keeping data needed for drain
// simulation (Section 5.2, NFR12). References to Secrets and ConfigMaps
// (ValueFrom) are kept, because they contain only names.
func Sanitize(s *snapshot.Snapshot) {
	if s == nil {
		return
	}

	sanitizeAll(s.Nodes, nil)
	sanitizeAll(s.Namespaces, nil)
	sanitizeAll(s.PDBs, nil)
	sanitizeAll(s.Pods, func(p *corev1.Pod) *corev1.PodSpec { return &p.Spec })
	sanitizeAll(s.ReplicaSets, func(r *appsv1.ReplicaSet) *corev1.PodSpec { return &r.Spec.Template.Spec })
	sanitizeAll(s.Deployments, func(d *appsv1.Deployment) *corev1.PodSpec { return &d.Spec.Template.Spec })
	sanitizeAll(s.StatefulSets, func(st *appsv1.StatefulSet) *corev1.PodSpec { return &st.Spec.Template.Spec })
	sanitizeAll(s.DaemonSets, func(d *appsv1.DaemonSet) *corev1.PodSpec { return &d.Spec.Template.Spec })
	sanitizeAll(s.Jobs, func(j *batchv1.Job) *corev1.PodSpec { return &j.Spec.Template.Spec })
}

// sanitizeAll cleans metadata of every item and, if podSpec is non-nil,
// removes env values from the pod spec it returns.
func sanitizeAll[T any, PT interface {
	*T
	metav1.Object
}](items []T, podSpec func(PT) *corev1.PodSpec) {
	for i := range items {
		obj := PT(&items[i])
		obj.SetManagedFields(nil)
		delete(obj.GetAnnotations(), corev1.LastAppliedConfigAnnotation)

		if podSpec != nil {
			redactPodSpec(podSpec(obj))
		}
	}
}

// redactPodSpec removes env values from all containers of the pod spec.
func redactPodSpec(spec *corev1.PodSpec) {
	for i := range spec.Containers {
		redactEnv(spec.Containers[i].Env)
	}
	for i := range spec.InitContainers {
		redactEnv(spec.InitContainers[i].Env)
	}
	for i := range spec.EphemeralContainers {
		redactEnv(spec.EphemeralContainers[i].Env)
	}
}

// redactEnv clears the literal value of every env var. ValueFrom is kept.
func redactEnv(env []corev1.EnvVar) {
	for i := range env {
		env[i].Value = ""
	}
}
