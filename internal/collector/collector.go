// Package collector reads a live Kubernetes cluster into a snapshot.Snapshot.
package collector

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/snapshot"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

// Options configures a KubernetesCollector.
type Options struct {
	// Kubeconfig is the path to the kubeconfig file. Empty uses the default.
	Kubeconfig string
	// KubeContext is the kubeconfig context to use. Empty uses the current one.
	KubeContext string
	// PrometheusURL is the base URL of the Prometheus HTTP API to read the
	// usage of the workloads from (FR20). Empty means no usage profiles.
	PrometheusURL string
	// UsageWindow is the length of the usage history to read. Zero means
	// DefaultUsageWindow.
	UsageWindow time.Duration
}

// KubernetesCollector reads the cluster state through the Kubernetes API.
type KubernetesCollector struct {
	client kubernetes.Interface // read-only access to the API server
	prom   *promClient          // nil without Options.PrometheusURL
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
	c := &KubernetesCollector{client: client, opts: opts}
	if opts.PrometheusURL != "" {
		// Queries over a week of history can take long; the context of
		// Collect still cancels them on Ctrl+C.
		c.prom = &promClient{baseURL: opts.PrometheusURL, http: &http.Client{Timeout: 5 * time.Minute}}
	}
	if c.opts.UsageWindow == 0 {
		c.opts.UsageWindow = DefaultUsageWindow
	}
	return c
}

// Collect returns a complete snapshot of the cluster, or an error if any
// API request fails. With a Prometheus URL it also reads the usage profiles
// of the workloads (FR20); a failed Prometheus query is an error too. It
// never returns a partial snapshot.
func (c *KubernetesCollector) Collect(ctx context.Context) (*snapshot.Snapshot, error) {
	version, err := c.client.Discovery().ServerVersion()
	if err != nil {
		return nil, fmt.Errorf("reading server version: %w", err)
	}

	snap := &snapshot.Snapshot{
		FormatVersion:     snapshot.FormatVersion,
		CreatedAt:         time.Now().UTC(),
		KubernetesVersion: version.GitVersion,
	}

	// Cluster-scoped objects have no namespace (unique name across cluster).
	nodes, err := c.client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing nodes: %w", err)
	}
	snap.Nodes = nodes.Items

	namespaces, err := c.client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing namespaces: %w", err)
	}
	snap.Namespaces = namespaces.Items

	// Namespaced objects are read from all namespaces at once.
	pods, err := c.client.CoreV1().Pods(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing pods: %w", err)
	}
	snap.Pods = pods.Items

	pdbs, err := c.client.PolicyV1().PodDisruptionBudgets(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing pdbs: %w", err)
	}
	snap.PDBs = pdbs.Items

	replicaSets, err := c.client.AppsV1().ReplicaSets(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing replicasets: %w", err)
	}
	snap.ReplicaSets = replicaSets.Items

	deployments, err := c.client.AppsV1().Deployments(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing deployments: %w", err)
	}
	snap.Deployments = deployments.Items

	statefulSets, err := c.client.AppsV1().StatefulSets(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing statefulsets: %w", err)
	}
	snap.StatefulSets = statefulSets.Items

	daemonSets, err := c.client.AppsV1().DaemonSets(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing daemonsets: %w", err)
	}
	snap.DaemonSets = daemonSets.Items

	jobs, err := c.client.BatchV1().Jobs(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing jobs: %w", err)
	}
	snap.Jobs = jobs.Items

	if c.prom != nil {
		// Evaluated at the time of the snapshot, so usage and objects
		// describe the same moment.
		profiles, err := collectUsage(ctx, c.prom, c.opts.UsageWindow, snap.CreatedAt, snap.ReplicaSets)
		if err != nil {
			return nil, err
		}
		snap.UsageProfiles = profiles
	}
	// Sanitize.
	Sanitize(snap)
	return snap, nil
}
