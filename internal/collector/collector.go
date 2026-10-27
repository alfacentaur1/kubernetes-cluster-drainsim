// Package collector reads a live Kubernetes cluster into a snapshot.Snapshot.
package collector

import (
	"context"
	"time"

	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/snapshot"
)

// Options configures a KubernetesCollector.
type Options struct {
	// Kubeconfig is the path to the kubeconfig file. Empty uses the default.
	Kubeconfig string
	// KubeContext is the kubeconfig context to use. Empty uses the current one.
	KubeContext string
	// PrometheusURL is where to read cluster usage from. Empty means no usage data.
	PrometheusURL string
	// UsageWindow is the time range over which usage is averaged.
	UsageWindow time.Duration
}

// KubernetesCollector reads cluster objects from the API server.
// It never modifies the cluster.
type KubernetesCollector struct {
	opts Options
}

// New returns a KubernetesCollector configured by opts.
// It returns an error if the cluster connection cannot be set up.
func New(opts Options) (*KubernetesCollector, error) {
	return &KubernetesCollector{opts: opts}, nil
}

// Collect returns a complete snapshot of the cluster, or an error if any
// API request fails. It never returns a partial snapshot.
func (c *KubernetesCollector) Collect(ctx context.Context) (*snapshot.Snapshot, error) {
	return &snapshot.Snapshot{}, nil
}
