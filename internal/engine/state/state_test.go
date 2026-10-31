package state

import (
	"slices"
	"testing"

	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/snapshot"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func pod(namespace, name, nodeName string, phase corev1.PodPhase) corev1.Pod {
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name},
		Spec:       corev1.PodSpec{NodeName: nodeName},
		Status:     corev1.PodStatus{Phase: phase},
	}
}

func TestNodesAndBoundPods(t *testing.T) {
	s := New(&snapshot.Snapshot{
		Nodes: []corev1.Node{
			{ObjectMeta: metav1.ObjectMeta{Name: "node-b"}},
			{ObjectMeta: metav1.ObjectMeta{Name: "node-a"}},
		},
		Pods: []corev1.Pod{
			pod("b", "web", "node-a", corev1.PodRunning),
			pod("a", "web", "node-b", corev1.PodRunning),
			pod("a", "api", "node-a", corev1.PodPending),
			pod("a", "pending", "", corev1.PodPending),
			pod("a", "done", "node-a", corev1.PodSucceeded),
			pod("a", "crashed", "node-b", corev1.PodFailed),
		},
	})

	var nodes []string
	for _, n := range s.Nodes() {
		nodes = append(nodes, n.Name)
	}
	if want := []string{"node-a", "node-b"}; !slices.Equal(nodes, want) {
		t.Errorf("Nodes() = %v, want %v", nodes, want)
	}

	var pods []string
	for _, p := range s.BoundPods() {
		pods = append(pods, p.Namespace+"/"+p.Name)
	}
	if want := []string{"a/api", "a/web", "b/web"}; !slices.Equal(pods, want) {
		t.Errorf("BoundPods() = %v, want %v", pods, want)
	}
}
