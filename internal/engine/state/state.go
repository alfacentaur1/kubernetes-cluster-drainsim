package state

import (
	"cmp"
	"fmt"
	"maps"
	"slices"

	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/snapshot"
	corev1 "k8s.io/api/core/v1"
	resourcehelper "k8s.io/component-helpers/resource"
)

// ClusterState is the in-memory copy of the cluster that the simulation
// changes step by step (component Cluster state, Section 5.3). It holds its
// own copies of the objects, so the snapshot is never modified. Its content
// can be saved and restored with Save and Restore (Memento pattern).
// All returned lists are sorted, so the simulation is deterministic (NFR7).
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
	return slices.Sorted(maps.Keys(s.nodes))
}

// Node returns the node with the given name, or nil if there is no such node.
func (s *ClusterState) Node(name string) *corev1.Node {
	return s.nodes[name]
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

// Cordon marks the node as unschedulable, as kubectl cordon does.
// It returns an error if the node does not exist.
func (s *ClusterState) Cordon(name string) error {
	node, ok := s.nodes[name]
	if !ok {
		return fmt.Errorf("node %q not found", name)
	}
	node.Spec.Unschedulable = true
	return nil
}

// RemovePod removes an evicted pod from the state.
// It returns an error if the pod does not exist.
func (s *ClusterState) RemovePod(namespace, name string) error {
	key := podKey(namespace, name)
	if _, ok := s.pods[key]; !ok {
		return fmt.Errorf("pod %s/%s not found", namespace, name)
	}
	delete(s.pods, key)
	return nil
}

// AddPod stores a copy of a new pod, for example a replacement created by a
// controller. A replacement is usually pending (no node yet) and is placed
// later with Bind. It returns an error if a pod with the same namespace and
// name already exists.
func (s *ClusterState) AddPod(pod *corev1.Pod) error {
	key := podKey(pod.Namespace, pod.Name)
	if _, ok := s.pods[key]; ok {
		return fmt.Errorf("pod %s/%s already exists", pod.Namespace, pod.Name)
	}
	s.pods[key] = pod.DeepCopy()
	return nil
}

// Bind places a pod on a node by setting its spec.nodeName, as the
// scheduler's Bind step does. It returns an error if the pod or the node
// does not exist.
func (s *ClusterState) Bind(namespace, name, nodeName string) error {
	pod, ok := s.pods[podKey(namespace, name)]
	if !ok {
		return fmt.Errorf("pod %s/%s not found", namespace, name)
	}
	if _, ok := s.nodes[nodeName]; !ok {
		return fmt.Errorf("node %q not found", nodeName)
	}
	pod.Spec.NodeName = nodeName
	return nil
}

// Requested returns the sum of the resource requests of all pods bound to
// the node. It returns nil if the node does not exist.
func (s *ClusterState) Requested(nodeName string) corev1.ResourceList {
	if _, ok := s.nodes[nodeName]; !ok {
		return nil
	}
	sum := corev1.ResourceList{}
	for _, pod := range s.PodsOnNode(nodeName) {
		if pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
			continue
		}
		req := resourcehelper.PodRequests(pod, resourcehelper.PodResourcesOptions{})
		for name, v := range req {
			q := sum[name]
			q.Add(v)
			sum[name] = q
		}
	}
	return sum
}
