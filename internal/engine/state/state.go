package state

import (
	"cmp"

	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/snapshot"
	corev1 "k8s.io/api/core/v1"

	"maps"
	"slices"
)

// ClusterState is the in-memory copy of the cluster that the simulation
// changes step by step. It holds
// its own copies of the objects, so the snapshot is never modified.
// Saving and restoring the state (Memento) is added later.
// Note: return values are sorted, for outputs to be deterministic.
type ClusterState struct {
	nodes map[string]*corev1.Node // by node name
	pods  map[string]*corev1.Pod  // by "namespace/name"
}

// podKey returns the key of a pod in ClusterState.pods.
func podKey(namespace, name string) string {
	return namespace + "/" + name
}

// New builds the cluster state from a loaded snapshot. Nodes and pods are
// deep-copied, so later changes to the state do not affect the snapshot.
func New(snap *snapshot.Snapshot) *ClusterState {
	s := &ClusterState{
		nodes: make(map[string]*corev1.Node, len(snap.Nodes)),
		pods:  make(map[string]*corev1.Pod, len(snap.Pods)),
	}
	for i := range snap.Nodes {
		s.nodes[snap.Nodes[i].Name] = snap.Nodes[i].DeepCopy()
	}

	for i := range snap.Pods {
		p := &snap.Pods[i]
		s.pods[podKey(p.Namespace, p.Name)] = p.DeepCopy()
	}
	return s
}

// NodeNames returns the names of all nodes in ascending order.
func (s *ClusterState) NodeNames() []string {
	names := slices.Sorted(maps.Keys(s.nodes))
	return names
}

// Node returns the node with the given name, or nil if there is no such node.
func (s *ClusterState) Node(name string) *corev1.Node {
	if node, ok := s.nodes[name]; ok {
		return node
	}
	return nil
}

// PodsOnNode returns the pods bound to the given node, sorted by namespace
// and then by name. Pending pods (with no node) are never returned.
func (s *ClusterState) PodsOnNode(name string) []*corev1.Pod {
	pods := []*corev1.Pod{}
	for _, pod := range s.pods {
		if pod.Spec.NodeName == name {
			pods = append(pods, pod)
		}
	}

	slices.SortFunc(pods, func(a, b *corev1.Pod) int {
		return cmp.Or(
			cmp.Compare(a.Namespace, b.Namespace),
			cmp.Compare(a.Name, b.Name),
		)
	})

	return pods
}
