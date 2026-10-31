package state

import corev1 "k8s.io/api/core/v1"

// Memento is a saved copy of a ClusterState (Memento pattern, Section 5.3).
// It is opaque: only ClusterState can create and restore it.
type Memento struct {
	nodes map[string]*corev1.Node
	pods  map[string]*corev1.Pod
}

// Save returns a copy of the current state that Restore can return to.
func (s *ClusterState) Save() *Memento {
	return &Memento{
		nodes: cloneNodes(s.nodes),
		pods:  clonePods(s.pods),
	}
}

// Restore sets the state back to the saved copy. The same memento can be
// restored more than once.
func (s *ClusterState) Restore(m *Memento) {
	s.nodes = cloneNodes(m.nodes)
	s.pods = clonePods(m.pods)
}

// cloneNodes returns a new map with a deep copy of every node.
func cloneNodes(in map[string]*corev1.Node) map[string]*corev1.Node {
	out := make(map[string]*corev1.Node, len(in))
	for name, node := range in {
		out[name] = node.DeepCopy()
	}
	return out
}

// clonePods returns a new map with a deep copy of every pod.
func clonePods(in map[string]*corev1.Pod) map[string]*corev1.Pod {
	out := make(map[string]*corev1.Pod, len(in))
	for name, pod := range in {
		out[name] = pod.DeepCopy()
	}
	return out
}
