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

// Decide applies the kubectl drain rules to a pod (component Eviction rules)
func Decide(pod *corev1.Pod) Decision {
	// TODO 1: finished pod -> Evict
	if pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
		return Decision{
			Action: Skip,
			Reason: "pod terminated",
		}
	}
	// TODO 2: mirror pod -> Skip
	if _, ok := pod.Annotations[corev1.MirrorPodAnnotationKey]; ok {
		return Decision{
			Action: Skip,
			Reason: "pod is mirror",
		}
	}
	owner := metav1.GetControllerOf(pod)
	if owner == nil {
		return Decision{
			Action: Skip,
			Reason: "pod has no owner",
		}
	}

	// TODO 4: owner is a DaemonSet -> Skip
	if owner.Kind == "DaemonSet" {
		return Decision{
			Action: Skip,
			Reason: "pod is daemon",
		}
	}
	// TODO 5: no owner -> Refuse

	// TODO 6: emptyDir volume -> Refuse
	for _, v := range pod.Spec.Volumes {
		if(v.EmptyDir != nil){
			return Decision{
				Action: Skip,
				Reason: "pod has EmptyDir volume",
			}
		}
	}
	// TODO 7: otherwise -> Evict
	return Decision{
		Action: Evict,
		Reason: "all rules pass",
	}
}
