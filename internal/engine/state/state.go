package state

import (
	corev1 "k8s.io/api/core/v1"
)

// ClusterState is the in-memory copy of the cluster that the simulation
// changes step by step (component Cluster state, Section 5.3). It holds
// its own copies of the objects, so the snapshot is never modified.
// Saving and restoring the state (Memento) is added later.
type ClusterState struct {
	nodes map[string]*corev1.Node // by node name
	pods  map[string]*corev1.Pod  // by "namespace/name"
}
