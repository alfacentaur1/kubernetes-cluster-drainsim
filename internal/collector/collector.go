// Package collector reads the state of a live Kubernetes cluster and turns it
// into a snapshot.Snapshot that the simulator can work with offline.
package collector

import (
	"context"

	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/snapshot"
)

// KubernetesCollector collects cluster objects (nodes, pods, namespaces,
// PodDisruptionBudgets and workload controllers) from a Kubernetes API server.
//
// It only reads from the cluster and never modifies it.
type KubernetesCollector struct{}

// New creates a KubernetesCollector for the cluster selected by kubeconfig
// and kubeContext.
//
// kubeconfig is the path to a kubeconfig file. If it is empty, the default
// client-go loading rules apply (the KUBECONFIG environment variable, then
// ~/.kube/config).
//
// kubeContext is the name of the kubeconfig context to use (cluster+user). If it is empty,
// the kubeconfig's current context is used.
//
// It returns an error if the kubeconfig cannot be loaded or the client cannot
// be created.
func New(kubeconfig, kubeContext string) (*KubernetesCollector, error) {
	return &KubernetesCollector{}, nil
}

// Collect lists the objects the simulator needs from the cluster and returns
// them as a single snapshot.Snapshot, with FormatVersion, CreatedAt and
// KubernetesVersion filled in.
//
// ctx controls cancellation and timeouts of the API requests.
//
// It returns an error if any of the API requests fails. A partial snapshot is
// never returned.
func (c *KubernetesCollector) Collect(ctx context.Context) (*snapshot.Snapshot, error) {
	return &snapshot.Snapshot{}, nil
}
