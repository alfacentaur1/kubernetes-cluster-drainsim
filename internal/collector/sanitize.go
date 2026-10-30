package collector

import (
	"strings"

	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/snapshot"
	corev1 "k8s.io/api/core/v1"
)

// Sanitize removes sensitive literal environment variable values
// from a snapshot while preserving data needed for drain simulation.
func Sanitize(s *snapshot.Snapshot) {
	if s == nil {
		return
	}

	for i := range s.Pods {
		sanitizePodSpec(&s.Pods[i].Spec)
	}

	for i := range s.Deployments {
		sanitizePodSpec(&s.Deployments[i].Spec.Template.Spec)
	}

	for i := range s.ReplicaSets {
		sanitizePodSpec(&s.ReplicaSets[i].Spec.Template.Spec)
	}

	for i := range s.StatefulSets {
		sanitizePodSpec(&s.StatefulSets[i].Spec.Template.Spec)
	}

	for i := range s.DaemonSets {
		sanitizePodSpec(&s.DaemonSets[i].Spec.Template.Spec)
	}

	for i := range s.Jobs {
		sanitizePodSpec(&s.Jobs[i].Spec.Template.Spec)
	}
}

// sanitizeContainer goes through env variables of container and redacts sensitive values.
func sanitizePodSpec(spec *corev1.PodSpec) {
	for i := range spec.Containers {
		sanitizeContainer(&spec.Containers[i])
	}

	for i := range spec.InitContainers {
		sanitizeContainer(&spec.InitContainers[i])
	}

	for i := range spec.EphemeralContainers {
		sanitizeEnvVars(spec.EphemeralContainers[i].Env)
	}
}

func sanitizeContainer(container *corev1.Container) {
	sanitizeEnvVars(container.Env)
}

func sanitizeEnvVars(envVars []corev1.EnvVar) {
	for i := range envVars {
		if envVars[i].Value != "" && isSensitiveName(envVars[i].Name) {
			envVars[i].Value = "REDACTED"
		}
	}
}

func isSensitiveName(name string) bool {
	name = strings.ToUpper(name)

	sensitiveKeywords := []string{
		"PASSWORD",
		"PASSWD",
		"SECRET",
		"TOKEN",
		"API_KEY",
		"APIKEY",
		"ACCESS_KEY",
		"PRIVATE_KEY",
		"CREDENTIAL",
		"CLIENT_SECRET",
	}

	for _, keyword := range sensitiveKeywords {
		if strings.Contains(name, keyword) {
			return true
		}
	}

	return false
}
