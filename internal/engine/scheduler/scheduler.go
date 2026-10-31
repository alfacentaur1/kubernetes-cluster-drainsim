package scheduler

import (
	"context"
	"fmt"

	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
	fwk "k8s.io/kube-scheduler/framework"
	"k8s.io/kubernetes/pkg/scheduler/apis/config/latest"
	"k8s.io/kubernetes/pkg/scheduler/backend/cache"
	"k8s.io/kubernetes/pkg/scheduler/framework"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/names"
	frameworkruntime "k8s.io/kubernetes/pkg/scheduler/framework/runtime"
	schedulermetrics "k8s.io/kubernetes/pkg/scheduler/metrics"
)

// Adapter places pods with the plugins of the real kube-scheduler
// (component Scheduler adapter, Adapter pattern, Section 5.3).
type Adapter struct {
	fw     framework.Framework // the scheduler plugins, ready to be called
	lister *lister             // the cluster data the plugins read
}

// lister holds the cluster data that the plugins read. Its content is
// replaced before every placement, because the cluster state changes
// during the simulation.
type lister struct {
	fwk.SharedLister
}

// New prepares the scheduler plugins. It is called once per simulation.
func New(ctx context.Context) (*Adapter, error) {
	// Without this the framework panics: its plugins record metrics.
	schedulermetrics.InitMetrics()

	// The default configuration of kube-scheduler, with its default plugins.
	cfg, err := latest.Default()
	if err != nil {
		return nil, fmt.Errorf("default scheduler config: %w", err)
	}
	profile := &cfg.Profiles[0]

	// Remove the DynamicResources plugin: it needs data the snapshot
	// does not have and would crash (docs/decisions.md).
	enabled := profile.Plugins.MultiPoint.Enabled[:0]
	for _, p := range profile.Plugins.MultiPoint.Enabled {
		if p.Name != names.DynamicResources {
			enabled = append(enabled, p)
		}
	}
	profile.Plugins.MultiPoint.Enabled = enabled

	// The plugins read nodes and pods from here. It starts empty;
	// Place fills it before each placement.
	l := &lister{SharedLister: cache.NewEmptySnapshot()}

	// Some plugins need a client during setup. A fake one is enough:
	// it is never started and never talks to a cluster.
	client := fake.NewClientset()

	fw, err := frameworkruntime.NewFramework(ctx, plugins.NewInTreeRegistry(), profile,
		frameworkruntime.WithClientSet(client),
		frameworkruntime.WithInformerFactory(informers.NewSharedInformerFactory(client, 0)),
		frameworkruntime.WithSnapshotSharedLister(l),
	)
	if err != nil {
		return nil, fmt.Errorf("creating scheduler framework: %w", err)
	}
	return &Adapter{fw: fw, lister: l}, nil
}
