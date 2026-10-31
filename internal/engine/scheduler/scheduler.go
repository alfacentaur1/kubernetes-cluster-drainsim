package scheduler

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	fwk "k8s.io/kube-scheduler/framework"
	"k8s.io/kubernetes/pkg/scheduler/framework"
)

// lister is the SharedLister the framework reads from. Place swaps the
// snapshot inside it before each placement, so one framework can be used
// for the whole simulation while the cluster state changes.
type lister struct {
	fwk.SharedLister
}

// Adapter places pods with the plugins of the real kube-scheduler
// (component Scheduler adapter, Adapter pattern).
type Adapter struct {
	fw     framework.Framework
	lister *lister
}

// Result is the outcome of one placement.
type Result struct {
	// Node is the chosen node, or empty if the pod fits nowhere.
	Node string
	// Rejected holds, for each rejected node, the plugin and its reason.
	Rejected map[string]Rejection
}

// Rejection says which Filter plugin rejected a node and why.
type Rejection struct {
	Plugin string
	Reason string
}

// New builds the scheduler framework once (see newFramework in the spike).
func New(ctx context.Context) (*Adapter, error){
	return Adapter{
		nil,nil
	},nil

}

// Place finds a node for the pod among the given nodes, with the given
// pods already running on them. It never changes its arguments.
func (a *Adapter) Place(ctx context.Context, pod *corev1.Pod, nodes []*corev1.Node, pods []*corev1.Pod) (Result, error){
	return nil, nil
}

