package collector

import (
	"slices"
	"strings"

	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/snapshot"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var sensitiveKeywords = []string{
	"PASSWORD", "PASSWD", "SECRET", "TOKEN", "API_KEY",
	"APIKEY", "ACCESS_KEY", "PRIVATE_KEY", "CREDENTIAL",
}

// Sanitize removes managed fields, last-applied-configuration annotations and
// sensitive literal env values from a snapshot, keeping data needed for drain simulation.
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
// redacts sensitive env values in the pod spec it returns.
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

func redactPodSpec(spec *corev1.PodSpec) {
	for _, c := range spec.Containers {
		redactEnv(c.Env)
	}
	for _, c := range spec.InitContainers {
		redactEnv(c.Env)
	}
	for _, c := range spec.EphemeralContainers {
		redactEnv(c.Env)
	}
}

// redactEnv modifies env in place; the slice shares its backing array with the container.
func redactEnv(env []corev1.EnvVar) {
	for i := range env {
		if env[i].Value != "" && isSensitiveName(env[i].Name) {
			env[i].Value = "REDACTED"
		}
	}
}

func isSensitiveName(name string) bool {
	name = strings.ToUpper(name)
	return slices.ContainsFunc(sensitiveKeywords, func(k string) bool {
		return strings.Contains(name, k)
	})
}
