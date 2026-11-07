package engine

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/engine/eventlog"
	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/engine/eviction"
	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/engine/pdb"
	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/engine/psa"
	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/engine/replacement"
	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/engine/scheduler"
	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/engine/state"
	corev1 "k8s.io/api/core/v1"
	resourcehelper "k8s.io/component-helpers/resource"
)

// Placement finds a node for a pod (IPlacement, Adapter pattern, Section 5.3).
// It is implemented by scheduler.Adapter.
type Placement interface {
	Place(ctx context.Context, pod *corev1.Pod, st *state.ClusterState) (scheduler.Result, error)
}

// PolicyChecker checks a pod against a policy (IPolicyChecker, Strategy
// pattern, Section 5.3). It is implemented by psa.Checker.
type PolicyChecker interface {
	Check(pod *corev1.Pod, ns *corev1.Namespace) psa.Result
}

// Violation is a workload whose running pods violate a policy (FR30, S1).
// Pods of one workload share their template, so they are reported together.
type Violation struct {
	Namespace string   `json:"namespace"`
	Kind      string   `json:"kind"` // e.g. "Deployment", or "Pod" for a pod without a controller
	Name      string   `json:"name"`
	Pods      []string `json:"pods"`
	Level     string   `json:"level"`    // level checked against, e.g. "restricted:latest"
	Controls  string   `json:"controls"` // violated controls, from the first pod (S3)
}

// Simulation is the interface the analyses use (ISimulation, Section 5.3).
// Replacing container requests (Section 5.5) is added with consolidation.
type Simulation interface {
	SetPolicy(p PolicyChecker)
	Drain(ctx context.Context, nodes []string) (Result, error)
	Save() *Checkpoint
	Restore(cp *Checkpoint)
	Info() Info

	// Attach adds an observer that receives the events of later drains
	// (Observer pattern). The drain order search attaches the event log only
	// to the final order, so failed attempts do not appear in the report.
	Attach(o eventlog.Observer)
	// Detach removes an observer added by Attach.
	Detach(o eventlog.Observer)

	// MoveRequests returns the total requests of the pods that a drain of
	// the node would move in the current state: pods the eviction rules
	// evict and that are still running. The greedy and backtracking
	// strategies try the nodes with the smallest requests first (Section 5.4).
	MoveRequests(node string) corev1.ResourceList

	// CheckRunning checks every running pod against the policy and groups
	// the violations by workload (FR30).
	CheckRunning(p PolicyChecker) []Violation

	// RemoveNode removes a drained node from the cluster state (Algorithm 5.3).
	RemoveNode(node string) error
	// Requested returns the total requests of all pods on the node, which
	// the consolidation sorts the nodes by (Section 5.5).
	Requested(node string) corev1.ResourceList
	// Allocatable returns the resources of the node that pods can use.
	Allocatable(node string) corev1.ResourceList
	// Node returns a copy of the node, or nil if it does not exist. The
	// consolidation passes it to the cost model (Section 5.5).
	Node(name string) *corev1.Node
}

// Result is the outcome of draining nodes in the given order. The drain stops
// at the first node that cannot be drained.
type Result struct {
	Drained []string // nodes that were drained, in order
	Failed  string   // node whose drain failed, empty if all were drained
}

// Checkpoint is a saved state of the simulation (Memento pattern). Besides
// the cluster state it keeps the numbering of replacements, so a restored
// simulation gives the replacements the same names again (NFR7).
type Checkpoint struct {
	state       *state.Memento
	replacement replacement.Model
}

// Engine is the facade of the simulation engine (Facade pattern, Section 5.3).
// It calls the components in the order of Figure 5.4.
type Engine struct {
	state       *state.ClusterState
	placement   Placement
	policy      PolicyChecker
	replacement replacement.Model
	observers   []eventlog.Observer
	info        Info // describes the snapshot; computed once in New
}

// New creates an engine over a loaded cluster state. The policy checker is
// required; psa.New(nil, "") checks against the levels set on the namespaces.
func New(st *state.ClusterState, p Placement, policy PolicyChecker, observers ...eventlog.Observer) *Engine {
	return &Engine{state: st, placement: p, policy: policy, observers: observers, info: newInfo(st)}
}

// SetPolicy sets the checker that replacements are checked against, for
// example one with target levels (Section 5.6).
func (e *Engine) SetPolicy(p PolicyChecker) { e.policy = p }

// Save returns a checkpoint that Restore can return to.
func (e *Engine) Save() *Checkpoint {
	return &Checkpoint{state: e.state.Save(), replacement: e.replacement}
}

// Restore returns the simulation to a checkpoint. It can be called more than
// once with the same checkpoint.
func (e *Engine) Restore(cp *Checkpoint) {
	e.state.Restore(cp.state)
	e.replacement = cp.replacement
}

// Drain simulates draining the nodes one after another in the given order.
// An error means the simulation could not run (for example an unknown node
// or a failing scheduler plugin), not that a drain failed.
func (e *Engine) Drain(ctx context.Context, nodes []string) (Result, error) {
	var res Result
	for _, node := range nodes {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		ok, err := e.drainNode(ctx, node)
		if err != nil {
			return res, err
		}
		if !ok {
			res.Failed = node
			return res, nil
		}
		res.Drained = append(res.Drained, node)
	}
	return res, nil
}

// outcome is what happened to one pod in one round.
type outcome int

const (
	skipped  outcome = iota // left on the node (DaemonSet, mirror pod)
	refused                 // kubectl drain would stop at it
	deferred                // the budget does not allow it yet
	evicted                 // removed from the node
)

// drainNode drains one node (Figure 5.4). It returns false if the drain fails.
func (e *Engine) drainNode(ctx context.Context, node string) (bool, error) {
	if err := e.state.Cordon(node); err != nil {
		return false, err
	}
	e.notify(eventlog.Event{Kind: eventlog.Cordoned, Node: node})

	var refusedPods []string
	round := e.state.PodsOnNode(node)
	for {
		var waiting []*corev1.Pod
		progress := false
		for _, pod := range round {
			out, err := e.drainPod(ctx, node, pod)
			if err != nil {
				return false, err
			}
			switch out {
			case refused:
				refusedPods = append(refusedPods, key(pod))
			case deferred:
				waiting = append(waiting, pod)
			case evicted:
				progress = true
			}
		}
		if len(waiting) == 0 {
			break // deferred pods left? -> no
		}
		if !progress {
			// progress in this round? -> no -> the drain fails
			e.notify(eventlog.Event{Kind: eventlog.DrainFailed, Node: node,
				Reason: "no pod could be evicted in a round: " + keys(waiting)})
			return false, nil
		}
		round = waiting // new round with the deferred pods
	}

	// kubectl drain fails while refused pods are left (Section 2.7, P9).
	if len(refusedPods) > 0 {
		e.notify(eventlog.Event{Kind: eventlog.DrainFailed, Node: node,
			Reason: "refused pods: " + strings.Join(refusedPods, ", ")})
		return false, nil
	}
	e.notify(eventlog.Event{Kind: eventlog.NodeDrained, Node: node})
	return true, nil
}

// drainPod handles one pod of a drained node, from the eviction rules to the
// placement of its replacement (Figure 5.4).
func (e *Engine) drainPod(ctx context.Context, node string, pod *corev1.Pod) (outcome, error) {
	// Eviction rules: evict, skip or refuse.
	d := eviction.Decide(pod)
	switch d.Action {
	case eviction.Skip:
		e.notify(eventlog.Event{Kind: eventlog.Skipped, Node: node, Pod: key(pod), Reason: d.Reason})
		return skipped, nil
	case eviction.Refuse:
		e.notify(eventlog.Event{Kind: eventlog.Refused, Node: node, Pod: key(pod), Reason: d.Reason})
		return refused, nil
	}

	// PDB model: allowed, or deferred to the next round.
	b := pdb.Evict(e.state, pod)
	if !b.Allowed {
		e.notify(eventlog.Event{Kind: eventlog.Deferred, Node: node, Pod: key(pod), Reason: b.Reason})
		return deferred, nil
	}

	// Evict the pod (budget - 1, done by pdb.Evict).
	if err := e.state.RemovePod(pod.Namespace, pod.Name); err != nil {
		return 0, err
	}
	e.notify(eventlog.Event{Kind: eventlog.Evicted, Node: node, Pod: key(pod), Reason: b.Reason})

	// Create the replacement according to the workload kind.
	repl, ok := e.replacement.Replace(pod)
	if !ok {
		return evicted, nil
	}

	// PSA check: a rejected replacement is a lost replica (FR31, S2).
	if r := e.policy.Check(repl, e.state.Namespace(repl.Namespace)); !r.Allowed {
		e.notify(eventlog.Event{Kind: eventlog.LostReplica, Node: node, Pod: key(repl),
			Reason: fmt.Sprintf("rejected by Pod Security level %s: %s", r.Level, r.Violations)})
		return evicted, nil
	}

	// Schedule the replacement (Filter, Score).
	res, err := e.placement.Place(ctx, repl, e.state)
	if err != nil {
		return 0, err
	}
	if err := e.state.AddPod(repl); err != nil {
		return 0, err
	}
	if res.Node == "" {
		// Mark the replacement as Pending (P2, P3).
		e.notify(eventlog.Event{Kind: eventlog.Pending, Node: node, Pod: key(repl),
			Reason: rejections(res.Rejected)})
		return evicted, nil
	}

	// Update the cluster state (budget + 1).
	if err := e.state.Bind(repl.Namespace, repl.Name, res.Node); err != nil {
		return 0, err
	}
	pdb.Recover(e.state, repl)
	e.notify(eventlog.Event{Kind: eventlog.Placed, Node: node, Pod: key(repl), Target: res.Node})
	return evicted, nil
}

// notify sends an event to every observer (Observer pattern).
func (e *Engine) notify(ev eventlog.Event) {
	for _, o := range e.observers {
		o.Notify(ev)
	}
}

// key returns "namespace/name" of a pod.
func key(p *corev1.Pod) string { return p.Namespace + "/" + p.Name }

// keys returns the keys of the pods, separated by commas.
func keys(pods []*corev1.Pod) string {
	out := make([]string, len(pods))
	for i, p := range pods {
		out[i] = key(p)
	}
	return strings.Join(out, ", ")
}

// rejections describes why the scheduler rejected each node, sorted by node
// name (FR19), e.g. "node-a: TaintToleration (...); node-b: NodeUnschedulable (...)".
func rejections(r map[string]scheduler.Rejection) string {
	parts := []string{}
	for _, name := range slices.Sorted(maps.Keys(r)) {
		parts = append(parts, fmt.Sprintf("%s: %s (%s)", name, r[name].Plugin, r[name].Reason))
	}
	return strings.Join(parts, "; ")
}

// Attach adds an observer that receives the events of later drains.
func (e *Engine) Attach(o eventlog.Observer) {
	e.observers = append(e.observers, o)
}

// Detach removes an observer added by Attach.
func (e *Engine) Detach(o eventlog.Observer) {
	e.observers = slices.DeleteFunc(e.observers, func(x eventlog.Observer) bool { return x == o })
}

// MoveRequests returns the total requests(cpu, memory) of the pods that a drain of the
// node would move. Skipped and refused pods stay on the node, and finished
// pods reserve nothing, so they are not counted.
func (e *Engine) MoveRequests(node string) corev1.ResourceList {
	sum := corev1.ResourceList{}
	// For every pod on node.
	for _, p := range e.state.PodsOnNode(node) {
		if p.Status.Phase == corev1.PodSucceeded || p.Status.Phase == corev1.PodFailed {
			continue // Skip or refuse.
		}
		if eviction.Decide(p).Action != eviction.Evict {
			continue
		}
		// Requests of 1 pod, counted similar as in scheduler.
		for name, v := range resourcehelper.PodRequests(p, resourcehelper.PodResourcesOptions{}) {
			q := sum[name]
			q.Add(v)
			sum[name] = q
		}
	}
	return sum
}

// CheckRunning checks every running pod against the policy and groups the
// violations by workload (Figure 5.6, steps 1 and 2; FR30). Running pods are
// not rejected by a new level, but each of them would be on its next
// restart (S1). The result is sorted by namespace, kind and name (NFR7).
func (e *Engine) CheckRunning(p PolicyChecker) []Violation {
	var out []Violation
	at := map[string]int{} // namespace/kind/name -> index in out

	for _, pod := range e.state.BoundPods() { // sorted by namespace and name
		r := p.Check(pod, e.state.Namespace(pod.Namespace))
		if r.Allowed {
			continue
		}
		kind, name := e.state.Workload(pod)
		k := pod.Namespace + "/" + kind + "/" + name
		i, ok := at[k]
		if !ok {
			out = append(out, Violation{Namespace: pod.Namespace, Kind: kind, Name: name,
				Level: r.Level, Controls: r.Violations})
			i = len(out) - 1
			at[k] = i
		}
		out[i].Pods = append(out[i].Pods, key(pod))
	}

	slices.SortFunc(out, func(a, b Violation) int {
		return cmp.Or(
			cmp.Compare(a.Namespace, b.Namespace),
			cmp.Compare(a.Kind, b.Kind),
			cmp.Compare(a.Name, b.Name),
		)
	})
	return out
}

// RemoveNode removes a drained node and the pods that stayed on it.
func (e *Engine) RemoveNode(node string) error { return e.state.RemoveNode(node) }

// Requested returns the total requests of all running pods on the node,
// including DaemonSet pods (Section 5.5: "the requests of its pods").
func (e *Engine) Requested(node string) corev1.ResourceList { return e.state.Requested(node) }

// Allocatable returns the allocatable resources of the node, or nil if the
// node does not exist.
func (e *Engine) Allocatable(node string) corev1.ResourceList {
	n := e.state.Node(node)
	if n == nil {
		return nil
	}
	return n.Status.Allocatable.DeepCopy()
}

// Node returns a deep copy of the node, or nil if the node does not exist.
func (e *Engine) Node(name string) *corev1.Node {
	n := e.state.Node(name)
	if n == nil {
		return nil
	}
	return n.DeepCopy()
}
