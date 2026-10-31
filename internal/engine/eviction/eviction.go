package eviction

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Action is what a drain does with a pod.
type Action int

const (
	// Evict means the pod is evicted and, if it has a controller, replaced.
	Evict Action = iota
	// Skip means the drain leaves the pod on the node (DaemonSet and mirror pods).
	Skip
	// Refuse means kubectl drain stops at the pod unless forced
	// (no controller, emptyDir volume).
	Refuse
)

// Decision is the result of the eviction rules for one pod.
type Decision struct {
	Action Action
	// Reason explains the decision for the report, e.g. "pod has no controller".
	Reason string
}

// Decide applies the kubectl drain rules to a pod. It follows the filters of kubectl drain
// (k8s.io/kubectl/pkg/drain/filters.go) in the same order, and the first
// rule that matches decides. DaemonSet pods are skipped as with
// --ignore-daemonsets; pods without a controller and pods with an emptyDir
// volume are refused, as without --force and --delete-emptydir-data.
func Decide(pod *corev1.Pod) Decision {
	finished := pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed
	owner := metav1.GetControllerOf(pod)

	// The DaemonSet controller would recreate the pod on the same node.
	// A finished DaemonSet pod can be removed.
	if owner != nil && owner.Kind == "DaemonSet" && owner.APIVersion == "apps/v1" && !finished {
		return Decision{Action: Skip, Reason: "pod is managed by a DaemonSet"}
	}

	// A mirror pod is run by the kubelet from a local file and cannot be
	// removed through the API server, even when it has finished.
	if _, ok := pod.Annotations[corev1.MirrorPodAnnotationKey]; ok {
		return Decision{Action: Skip, Reason: "mirror pod"}
	}

	// A finished pod runs nothing, so the remaining rules do not apply.
	if finished {
		return Decision{Action: Evict, Reason: "pod has finished"}
	}

	// The data in an emptyDir volume is lost when the pod leaves the node.
	for _, v := range pod.Spec.Volumes {
		if v.EmptyDir != nil {
			return Decision{Action: Refuse, Reason: "pod uses an emptyDir volume"}
		}
	}

	// Nothing would recreate a pod without a controller after eviction.
	if owner == nil {
		return Decision{Action: Refuse, Reason: "pod has no controller"}
	}

	return Decision{Action: Evict, Reason: "pod has a controller"}
}
