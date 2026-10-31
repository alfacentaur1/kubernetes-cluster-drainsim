package scheduler

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/engine/state"
	corev1 "k8s.io/api/core/v1"
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

// lister embeds fwk.SharedLister; Place swaps the snapshot inside it before
// each placement, so one framework serves the whole simulation.
type lister struct {
	fwk.SharedLister
}

// Result is the outcome of placing one pod.
type Result struct {
	Node     string               // chosen node, empty if the pod fits nowhere
	Rejected map[string]Rejection // node name -> rejecting Filter plugin
}

// Rejection says which plugin rejected a node and why.
type Rejection struct {
	Plugin, Reason string
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

// Place chooses a node for the pod with the scheduler plugins: PreFilter,
// Filter for each node in name order, PreScore and Score (incl.
// NormalizeScore). A score tie goes to the lowest node name, so the result is
// deterministic (NFR7). Place never changes the state; the caller binds the
// pod. An error means a plugin failed, not that the pod does not fit.
func (a *Adapter) Place(ctx context.Context, pod *corev1.Pod, st *state.ClusterState) (Result, error) {
	// The plugins read the current state through the lister.
	a.lister.SharedLister = cache.NewSnapshot(st.BoundPods(), st.Nodes())

	all, err := a.lister.NodeInfos().List()
	if err != nil {
		return Result{}, fmt.Errorf("listing nodes: %w", err)
	}
	// A pod bound to a node that is not in the state gets a NodeInfo
	// without a node; such entries are not candidates.
	all = slices.DeleteFunc(all, func(ni fwk.NodeInfo) bool { return ni.Node() == nil })
	slices.SortFunc(all, func(x, y fwk.NodeInfo) int { return strings.Compare(x.Node().Name, y.Node().Name) })

	res := Result{Rejected: map[string]Rejection{}}
	cycle := framework.NewCycleState()

	preRes, status, _ := a.fw.RunPreFilterPlugins(ctx, cycle, pod)
	if !status.IsSuccess() {
		if !status.IsRejected() {
			return Result{}, status.AsError()
		}
		for _, ni := range all {
			res.Rejected[ni.Node().Name] = rejection(status)
		}
		return res, nil
	}

	var feasible []fwk.NodeInfo
	for _, ni := range all {
		name := ni.Node().Name
		if !preRes.AllNodes() && !preRes.NodeNames.Has(name) {
			res.Rejected[name] = Rejection{Reason: "excluded by PreFilter result"}
			continue
		}
		// Only the first rejecting plugin is reported (docs/decisions.md).
		status := a.fw.RunFilterPlugins(ctx, cycle, pod, ni)
		if status.IsSuccess() {
			feasible = append(feasible, ni)
			continue
		}
		if !status.IsRejected() {
			return Result{}, status.AsError()
		}
		res.Rejected[name] = rejection(status)
	}
	if len(feasible) == 0 {
		return res, nil
	}

	if status := a.fw.RunPreScorePlugins(ctx, cycle, pod, feasible); !status.IsSuccess() {
		return Result{}, status.AsError()
	}
	scores, status := a.fw.RunScorePlugins(ctx, cycle, pod, feasible)
	if !status.IsSuccess() {
		return Result{}, status.AsError()
	}
	slices.SortFunc(scores, func(x, y fwk.NodePluginScores) int { return strings.Compare(x.Name, y.Name) })

	// Strictly greater, so a tie keeps the lowest node name.
	best := scores[0]
	for _, s := range scores[1:] {
		if s.TotalScore > best.TotalScore {
			best = s
		}
	}
	res.Node = best.Name
	return res, nil
}

// rejection converts the status of a rejecting plugin.
func rejection(status *fwk.Status) Rejection {
	return Rejection{Plugin: status.Plugin(), Reason: strings.Join(status.Reasons(), "; ")}
}
