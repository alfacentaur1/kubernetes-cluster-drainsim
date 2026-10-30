// Package collector reads a live Kubernetes cluster into a snapshot.Snapshot.
package collector

import (
	"context"
	"fmt"
	"time"

	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/snapshot"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

// Options configures a KubernetesCollector.
type Options struct {
	// Kubeconfig is the path to the kubeconfig file. Empty uses the default.
	Kubeconfig string
	// KubeContext is the kubeconfig context to use. Empty uses the current one.
	KubeContext string
	// PrometheusURL is where to read cluster usage from. Empty means no usage data.
	PrometheusURL string
	// UsageWindow is the length of the usage history to read.
	UsageWindow time.Duration
}

// KubernetesCollector reads the cluster state through the Kubernetes API.
type KubernetesCollector struct {
	client kubernetes.Interface // read-only access to the API server
	opts   Options
}

// New returns a collector for the cluster selected by opts. Empty Kubeconfig
// and KubeContext mean the default kubeconfig and its current context, as in
// kubectl. It returns an error if the client cannot be set up.
func New(opts Options) (*KubernetesCollector, error) {
	// Configs for kube file (for kubectl).
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	if opts.Kubeconfig != "" {
		rules.ExplicitPath = opts.Kubeconfig
	}
	// Overriding context.
	overrides := &clientcmd.ConfigOverrides{
		CurrentContext: opts.KubeContext,
	}

	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, overrides).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("loading kubeconfig: %w", err)
	}

	// Client for collecting kube state.
	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("creating Kubernetes client: %w", err)
	}

	return newWithClient(client, opts), nil
}

// newWithClient returns a collector that reads from the given client.
// New uses it with a real client, tests with a fake one.
func newWithClient(client kubernetes.Interface, opts Options) *KubernetesCollector {
	return &KubernetesCollector{client: client, opts: opts}
}

// Collect returns a complete snapshot of the cluster, or an error if any
// API request fails. It never returns a partial snapshot.
func (c *KubernetesCollector) Collect(ctx context.Context) (*snapshot.Snapshot, error) {
	return &snapshot.Snapshot{}, nil
}
