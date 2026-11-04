package pdb

import (
	"fmt"
	"strings"

	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/engine/state"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	podutil "k8s.io/kubernetes/pkg/api/v1/pod"
)

// Decision is the result of an eviction request checked against the
// PodDisruptionBudgets of the pod.
type Decision struct {
	// Allowed is true if the pod may be evicted now.
	Allowed bool
	// Permanent is true if the eviction can never be allowed (a pod covered
	// by more than one budget). Otherwise a refused eviction may be allowed
	// in a later round, after a replacement has been placed.
	Permanent bool
	// Reason explains the decision for the report.
	Reason string
}

// Evict checks whether the API server would allow the eviction of the pod
// (component PDB model, Section 5.3) and, if it is allowed, lowers the budget
// in the cluster state. The rules and their order follow the Eviction API in
// pkg/registry/core/pod/storage/eviction.go of Kubernetes 1.36.
func Evict(st *state.ClusterState, pod *corev1.Pod) Decision {
	// A pod that is not running yet or has finished is not protected.
	switch pod.Status.Phase {
	case corev1.PodPending, corev1.PodSucceeded, corev1.PodFailed:
		return Decision{Allowed: true, Reason: "pod is not running"}
	}

	budgets := st.PDBsForPod(pod)

	// The API server refuses a pod covered by more than one budget (P10).
	if len(budgets) > 1 {
		return Decision{Permanent: true, Reason: fmt.Sprintf(
			"pod is covered by %d PodDisruptionBudgets: %s", len(budgets), names(budgets))}
	}
	if len(budgets) == 0 {
		return Decision{Allowed: true, Reason: "no PodDisruptionBudget"}
	}
	b := budgets[0]

	// An unhealthy pod does not count towards the budget (P8).
	if !podutil.IsPodReady(pod) {
		policy := b.Spec.UnhealthyPodEvictionPolicy
		if policy != nil && *policy == policyv1.AlwaysAllow {
			return Decision{Allowed: true, Reason: "unhealthy pod, policy AlwaysAllow"}
		}
		if b.Status.CurrentHealthy >= b.Status.DesiredHealthy && b.Status.DesiredHealthy > 0 {
			return Decision{Allowed: true, Reason: "unhealthy pod, budget is met"}
		}
		// Otherwise it is checked like a healthy pod below.
	}

	// The budget is used up (P1, P7). The eviction may succeed in a later
	// round, once a replacement is placed.
	if b.Status.DisruptionsAllowed <= 0 {
		return Decision{Reason: fmt.Sprintf(
			"PodDisruptionBudget %s needs %d healthy pods and has %d",
			b.Name, b.Status.DesiredHealthy, b.Status.CurrentHealthy)}
	}

	b.Status.DisruptionsAllowed--
	if podutil.IsPodReady(pod) {
		b.Status.CurrentHealthy--
	}
	return Decision{Allowed: true, Reason: "PodDisruptionBudget " + b.Name + " allows it"}
}

// Recover restores the budgets of a replacement that has been placed. The
// replacement is assumed to become Ready (Section 5.3), so it counts as a
// healthy pod again (FR11).
func Recover(st *state.ClusterState, replacement *corev1.Pod) {
	for _, b := range st.PDBsForPod(replacement) {
		b.Status.CurrentHealthy++
		b.Status.DisruptionsAllowed = max(0, b.Status.CurrentHealthy-b.Status.DesiredHealthy)
	}
}

// names returns the names of the budgets, separated by commas.
func names(budgets []*policyv1.PodDisruptionBudget) string {
	out := make([]string, len(budgets))
	for i, b := range budgets {
		out[i] = b.Name
	}
	return strings.Join(out, ", ")
}
