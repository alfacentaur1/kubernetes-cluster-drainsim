// Spike: run the upstream kube-scheduler framework plugins in-process against
// an in-memory set of nodes and pods (no API server). For each pod it prints
// the chosen node, or, for an unschedulable pod, the rejecting Filter plugin
// and its reason per node.
//
// The wiring follows Cluster Autoscaler (simulator/framework/handle.go):
// in-tree registry + default profile + a snapshot as the SharedLister.
package main

import (
	"context"
	"fmt"
	"os"
	"sort"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
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

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	nodes := []*v1.Node{
		node("node-a", "2", "4Gi", nil),
		node("node-b", "4", "8Gi", []v1.Taint{{Key: "dedicated", Value: "db", Effect: v1.TaintEffectNoSchedule}}),
		node("node-c", "4", "8Gi", nil),
	}
	existing := []*v1.Pod{
		withNode(pod("default", "running-1", "1500m", "1Gi"), "node-a"),
		withNode(pod("default", "running-2", "3", "1Gi"), "node-c"),
	}

	picky := pod("default", "picky", "100m", "64Mi")
	picky.Spec.NodeSelector = map[string]string{"disk": "ssd"}

	pending := []*v1.Pod{
		pod("default", "small", "500m", "256Mi"), // fits node-a and node-c; node-b is tainted
		pod("default", "big", "8", "1Gi"),        // no node has 8 CPUs
		picky,                                    // no node has disk=ssd
	}

	snap := cache.NewSnapshot(existing, nodes)
	f, err := newFramework(ctx, snap)
	if err != nil {
		return err
	}

	// TODO(engine): each pod is scheduled against the initial snapshot; a placed
	// pod is not added to it, so later pods see the node's old free capacity.
	// The drain simulation must add every placed pod to the state.
	for _, p := range pending {
		res, err := schedule(ctx, f, snap, p)
		if err != nil {
			return fmt.Errorf("pod %s/%s: %w", p.Namespace, p.Name, err)
		}
		res.print(p)
	}
	return nil
}

// newFramework builds the default scheduler profile with all in-tree plugins.
// The fake clientset and informer factory are only needed because some plugins
// create listers during initialisation; they are never started or written to.
func newFramework(ctx context.Context, snap fwk.SharedLister) (framework.Framework, error) {
	// The framework records plugin metrics unconditionally; without this the
	// metric vectors are nil and NewFramework panics. InitMetrics only creates
	// them, it does not register them anywhere or expose an endpoint.
	schedulermetrics.InitMetrics()

	cfg, err := latest.Default()
	if err != nil {
		return nil, fmt.Errorf("default scheduler config: %w", err)
	}
	profile := &cfg.Profiles[0]

	// DynamicResources (DRA) dereferences a SharedDRAManager in every PreFilter.
	// The snapshot carries no ResourceClaims/DeviceClasses, so the plugin is
	// dropped instead of feeding it an empty manager (CA implements its own).
	enabled := profile.Plugins.MultiPoint.Enabled[:0]
	for _, p := range profile.Plugins.MultiPoint.Enabled {
		if p.Name != names.DynamicResources {
			enabled = append(enabled, p)
		}
	}
	profile.Plugins.MultiPoint.Enabled = enabled

	client := fake.NewClientset()
	return frameworkruntime.NewFramework(ctx, plugins.NewInTreeRegistry(), profile,
		frameworkruntime.WithClientSet(client),
		frameworkruntime.WithInformerFactory(informers.NewSharedInformerFactory(client, 0)),
		frameworkruntime.WithSnapshotSharedLister(snap),
	)
}

type result struct {
	node     string                 // chosen node, empty if unschedulable
	scores   []fwk.NodePluginScores // feasible nodes, sorted by name
	rejected map[string]*fwk.Status // node name -> status of the rejecting plugin
}

// schedule runs PreFilter, Filter, PreScore and Score (incl. NormalizeScore)
// for one pod. Nodes are evaluated in name order and score ties are broken by
// the lowest node name, so the result is deterministic.
func schedule(ctx context.Context, f framework.Framework, snap fwk.SharedLister, p *v1.Pod) (*result, error) {
	all, err := snap.NodeInfos().List()
	if err != nil {
		return nil, err
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Node().Name < all[j].Node().Name })

	res := &result{rejected: map[string]*fwk.Status{}}
	state := framework.NewCycleState()

	preRes, st, _ := f.RunPreFilterPlugins(ctx, state, p)
	if !st.IsSuccess() {
		if !st.IsRejected() {
			return nil, st.AsError()
		}
		for _, ni := range all {
			res.rejected[ni.Node().Name] = st
		}
		return res, nil
	}

	var feasible []fwk.NodeInfo
	for _, ni := range all {
		name := ni.Node().Name
		if !preRes.AllNodes() && !preRes.NodeNames.Has(name) {
			res.rejected[name] = fwk.NewStatus(fwk.UnschedulableAndUnresolvable, "excluded by PreFilter result")
			continue
		}
		// TODO(reporter): RunFilterPlugins stops at the first rejecting plugin, so
		// only one reason per node is reported (e.g. a taint hides Insufficient cpu).
		// Decide whether the report needs all reasons.
		st := f.RunFilterPlugins(ctx, state, p, ni)
		if st.IsSuccess() {
			feasible = append(feasible, ni)
			continue
		}
		if !st.IsRejected() {
			return nil, st.AsError()
		}
		res.rejected[name] = st
	}
	if len(feasible) == 0 {
		return res, nil
	}

	if st := f.RunPreScorePlugins(ctx, state, p, feasible); !st.IsSuccess() {
		return nil, st.AsError()
	}
	scores, st := f.RunScorePlugins(ctx, state, p, feasible)
	if !st.IsSuccess() {
		return nil, st.AsError()
	}
	sort.Slice(scores, func(i, j int) bool { return scores[i].Name < scores[j].Name })
	res.scores = scores

	best := scores[0]
	for _, s := range scores[1:] {
		if s.TotalScore > best.TotalScore {
			best = s
		}
	}
	res.node = best.Name
	return res, nil
}

func (r *result) print(p *v1.Pod) {
	fmt.Printf("pod %s/%s\n", p.Namespace, p.Name)
	for _, s := range r.scores {
		fmt.Printf("  %-8s score %d\n", s.Name, s.TotalScore)
	}
	names := make([]string, 0, len(r.rejected))
	for n := range r.rejected {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		st := r.rejected[n]
		fmt.Printf("  %-8s rejected by %s: %v\n", n, st.Plugin(), st.Reasons())
	}
	if r.node == "" {
		fmt.Println("  => unschedulable")
	} else {
		fmt.Printf("  => %s\n", r.node)
	}
}

func node(name, cpu, mem string, taints []v1.Taint) *v1.Node {
	alloc := v1.ResourceList{
		v1.ResourceCPU:    resource.MustParse(cpu),
		v1.ResourceMemory: resource.MustParse(mem),
		v1.ResourcePods:   resource.MustParse("110"),
	}
	return &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			UID:    types.UID("node-" + name),
			Labels: map[string]string{v1.LabelHostname: name},
		},
		Spec:   v1.NodeSpec{Taints: taints},
		Status: v1.NodeStatus{Capacity: alloc, Allocatable: alloc},
	}
}

func pod(ns, name, cpu, mem string) *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name, UID: types.UID("pod-" + ns + "-" + name)},
		Spec: v1.PodSpec{
			Containers: []v1.Container{{
				Name:  "app",
				Image: "registry.k8s.io/pause:3.10",
				Resources: v1.ResourceRequirements{Requests: v1.ResourceList{
					v1.ResourceCPU:    resource.MustParse(cpu),
					v1.ResourceMemory: resource.MustParse(mem),
				}},
			}},
		},
	}
}

func withNode(p *v1.Pod, nodeName string) *v1.Pod {
	p.Spec.NodeName = nodeName
	return p
}
