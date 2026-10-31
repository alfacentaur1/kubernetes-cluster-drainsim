package replacement

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Model creates replacements for evicted pods (component Replacement model). It numbers the replacements it creates, so new names are
// unique and the same simulation always produces the same names (NFR7).
type Model struct {
	next int // number of the next replacement
}

// creator builds the replacement of a pod owned by one kind of workload
type creator func(m *Model, pod *corev1.Pod) *corev1.Pod

// creators maps an owner kind to its creator. Kinds that are not listed
// get a pod with a new name, as a ReplicaSet or a Job would create.
var creators = map[string]creator{
	"StatefulSet": sameName,
}

func (m *Model) Replace(pod *corev1.Pod) (*corev1.Pod, bool) {
	// A finished pod has done its work and is not recreated.
	if pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
		return nil, false
	}

	// Nothing recreates a pod without a controller.
	owner := metav1.GetControllerOf(pod)
	if owner == nil {
		return nil, false
	}

	// Pick the creator for the owner kind (Factory Method).
	create, ok := creators[owner.Kind]
	if !ok {
		create = newName
	}
	return create(m, pod), true
}

// pendingCopy returns a copy of the pod as a controller would create it:
// without a node, without status and without the identity of the old pod.
func pendingCopy(pod *corev1.Pod) *corev1.Pod {
	p := pod.DeepCopy()
	p.Spec.NodeName = ""
	p.Status = corev1.PodStatus{Phase: corev1.PodPending}
	p.UID = ""
	p.ResourceVersion = ""
	return p
}

// newName returns a pending copy of the pod with a new name, as a
// ReplicaSet or a Job creates it. The name ends with a sequence number
// instead of a random suffix, so it is deterministic.
func newName(m *Model, pod *corev1.Pod) *corev1.Pod {
	p := pendingCopy(pod)
	base := pod.GenerateName
	if base == "" {
		base = pod.Name + "-"
	}
	p.Name = fmt.Sprintf("%ssim%d", base, m.next)
	m.next++
	return p
}

// sameName returns a pending copy of the pod with the same name,
// as a StatefulSet creates it.
func sameName(m *Model, pod *corev1.Pod) *corev1.Pod {
	return pendingCopy(pod)
}
