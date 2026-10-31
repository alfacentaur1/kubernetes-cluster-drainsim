package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/engine/state"
	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/snapshot"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/names"
)

func TestPlaceSkipsTaintedAndCordonedNodes(t *testing.T) {
	tainted := node("node-a")
	tainted.Spec.Taints = []corev1.Taint{{Key: "dedicated", Value: "db", Effect: corev1.TaintEffectNoSchedule}}
	cordoned := node("node-b")
	cordoned.Spec.Unschedulable = true
	free := node("node-c")

	st := state.New(&snapshot.Snapshot{
		CreatedAt:         time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		KubernetesVersion: "v1.36.5",
		Nodes:             []corev1.Node{*tainted, *cordoned, *free},
	})

	a, err := New(context.Background())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "web", UID: types.UID("pod-web")},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{
				Name:  "app",
				Image: "registry.k8s.io/pause:3.10",
				Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("100m"),
					corev1.ResourceMemory: resource.MustParse("64Mi"),
				}},
			}},
		},
	}

	res, err := a.Place(context.Background(), pod, st)
	if err != nil {
		t.Fatalf("Place() error = %v", err)
	}

	if res.Node != "node-c" {
		t.Errorf("Node = %q, want node-c", res.Node)
	}
	wantPlugins := map[string]string{
		"node-a": names.TaintToleration,
		"node-b": names.NodeUnschedulable,
	}
	if len(res.Rejected) != len(wantPlugins) {
		t.Errorf("Rejected = %v, want rejections for %v", res.Rejected, wantPlugins)
	}
	for nodeName, plugin := range wantPlugins {
		if got := res.Rejected[nodeName].Plugin; got != plugin {
			t.Errorf("Rejected[%s].Plugin = %q, want %q", nodeName, got, plugin)
		}
	}
	if pod.Spec.NodeName != "" {
		t.Errorf("Place changed the pod: nodeName = %q", pod.Spec.NodeName)
	}
}

func node(name string) *corev1.Node {
	alloc := corev1.ResourceList{
		corev1.ResourceCPU:    resource.MustParse("2"),
		corev1.ResourceMemory: resource.MustParse("4Gi"),
		corev1.ResourcePods:   resource.MustParse("110"),
	}
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			UID:    types.UID("node-" + name),
			Labels: map[string]string{corev1.LabelHostname: name},
		},
		Status: corev1.NodeStatus{Capacity: alloc, Allocatable: alloc},
	}
}
