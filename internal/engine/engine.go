package engine

import (
	"context"

	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/engine/eventlog"
	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/engine/replacement"
	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/engine/scheduler"
	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/engine/state"
	corev1 "k8s.io/api/core/v1"
)

// Placement finds a node for a pod (IPlacement, Section 5.3).
// It is implemented by scheduler.Adapter.
type Placement interface {
	Place(ctx context.Context, pod *corev1.Pod, st *state.ClusterState) (scheduler.Result, error)
}

// Engine is the facade of the simulation engine (ISimulation, Facade
// pattern, Section 5.3). Analyses use only its methods.
type Engine struct {
	state       *state.ClusterState
	placement   Placement
	replacement *replacement.Model
	observers   []eventlog.Observer
}

// New creates an engine over a loaded cluster state.
func New(st *state.ClusterState, p Placement, observers ...eventlog.Observer) *Engine {

}

// DrainResult is the outcome of draining one node.
type DrainResult struct {
	Drained bool // true if all evictable pods left the node
}

// Drain simulates draining one node (Figure 5.4).
func (e *Engine) Drain(ctx context.Context, node string) (DrainResult, error)

// notify sends an event to every observer.
func (e *Engine) notify(ev eventlog.Event)
