// Package collector reads a live Kubernetes cluster into a snapshot.Snapshot.
package collector

import (
	"context"
	"fmt"
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
	// Sanitize.
	Sanitize(snap)
	return snap, nil
}
