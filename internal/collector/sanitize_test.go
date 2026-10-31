package collector

import (
	"testing"

	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/snapshot"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

func TestSanitize(t *testing.T) {
	s := &snapshot.Snapshot{
		Pods: []corev1.Pod{
			{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Env: []corev1.EnvVar{
								{
									Name:  "DATABASE_PASSWORD",
									Value: "super-secret",
								},
								{
									Name:  "LOG_LEVEL",
									Value: "debug",
								},
							},
						},
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
								{
									Env: []corev1.EnvVar{
										{
											Name:  "API_TOKEN",
											Value: "my-token",
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	Sanitize(s)

	if got := s.Pods[0].Spec.Containers[0].Env[0].Value; got != "REDACTED" {
		t.Errorf("password was not sanitized: got %q", got)
	}

	if got := s.Pods[0].Spec.Containers[0].Env[1].Value; got != "debug" {
		t.Errorf("non-sensitive value changed: got %q", got)
	}

	if got := s.Deployments[0].Spec.Template.Spec.Containers[0].Env[0].Value; got != "REDACTED" {
		t.Errorf("deployment token was not sanitized: got %q", got)
	}
}

func TestSanitizeNil(t *testing.T) {
	Sanitize(nil)
}
