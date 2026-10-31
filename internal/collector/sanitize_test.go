package collector

import (
	"testing"

	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/snapshot"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestSanitize(t *testing.T) {
	secretRef := &corev1.EnvVarSource{
		SecretKeyRef: &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: "db"},
			Key:                  "password",
		},
	}

	s := &snapshot.Snapshot{
		Pods: []corev1.Pod{
			{
				ObjectMeta: metav1.ObjectMeta{
					ManagedFields: []metav1.ManagedFieldsEntry{{Manager: "kubectl"}},
					Annotations: map[string]string{
						corev1.LastAppliedConfigAnnotation: `{"spec":{}}`,
						"team":                             "payments",
					},
				},
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{
						{Env: []corev1.EnvVar{{Name: "MIGRATE", Value: "true"}}},
					},
					Containers: []corev1.Container{
						{
							Env: []corev1.EnvVar{
								{Name: "DATABASE_PASSWORD", Value: "super-secret"},
								{Name: "LOG_LEVEL", Value: "debug"},
								{Name: "DB_PASSWORD_REF", ValueFrom: secretRef},
							},
						},
					},
					EphemeralContainers: []corev1.EphemeralContainer{
						{EphemeralContainerCommon: corev1.EphemeralContainerCommon{
							Env: []corev1.EnvVar{{Name: "DEBUG", Value: "1"}},
						}},
					},
				},
			},
		},
		Deployments: []appsv1.Deployment{
			{
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{Env: []corev1.EnvVar{{Name: "API_TOKEN", Value: "my-token"}}},
							},
						},
					},
				},
			},
		},
	}

	Sanitize(s)

	pod := s.Pods[0]
	if pod.ManagedFields != nil {
		t.Errorf("managedFields were not removed: %v", pod.ManagedFields)
	}
	if _, ok := pod.Annotations[corev1.LastAppliedConfigAnnotation]; ok {
		t.Error("last-applied-configuration annotation was not removed")
	}
	if got := pod.Annotations["team"]; got != "payments" {
		t.Errorf("other annotation changed: got %q", got)
	}

	envs := [][]corev1.EnvVar{
		pod.Spec.InitContainers[0].Env,
		pod.Spec.Containers[0].Env,
		pod.Spec.EphemeralContainers[0].Env,
		s.Deployments[0].Spec.Template.Spec.Containers[0].Env,
	}
	for _, env := range envs {
		for _, e := range env {
			if e.Value != "" {
				t.Errorf("env %s: value was not removed: got %q", e.Name, e.Value)
			}
		}
	}

	ref := pod.Spec.Containers[0].Env[2].ValueFrom
	if ref == nil || ref.SecretKeyRef == nil || ref.SecretKeyRef.Name != "db" || ref.SecretKeyRef.Key != "password" {
		t.Errorf("valueFrom changed: got %+v", ref)
	}
}

func TestSanitizeNil(t *testing.T) {
	Sanitize(nil)
}
