package psa

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/pod-security-admission/api"
	"k8s.io/pod-security-admission/policy"
)

// Result is the outcome of checking one pod against a Pod Security level.
type Result struct {
	// Allowed is true if the pod meets the level.
	Allowed bool
	// Level is the level the pod was checked against, e.g. "restricted:latest".
	Level string
	// Violations lists the violated controls of the Pod Security Standards,
	// e.g. "privileged containers, host ports (8080)" (S3). Empty if allowed.
	Violations string
}

// Checker checks pods against the Pod Security level of their namespace with
// the Pod Security Admission library (component PSA checker, Adapter pattern,
// Section 5.3). Without target levels it uses the enforce level set on each
// namespace; target levels replace it (FR29, Section 5.6).
type Checker struct {
	evaluator policy.Evaluator
	targets   map[string]api.Level // target level per namespace
	cluster   *api.Level           // target level for all other namespaces, or nil
}

// New creates a checker. targets maps a namespace to its target level and
// cluster is the target level for every other namespace; both may be empty,
// in which case the levels set on the namespaces are used. Levels are
// "privileged", "baseline" or "restricted".
func New(targets map[string]string, cluster string) (*Checker, error) {
	ev, err := policy.NewEvaluator(policy.DefaultChecks(), nil)
	if err != nil {
		return nil, fmt.Errorf("creating Pod Security evaluator: %w", err)
	}
	c := &Checker{evaluator: ev, targets: map[string]api.Level{}}
	for ns, l := range targets {
		level, err := api.ParseLevel(l)
		if err != nil {
			return nil, fmt.Errorf("target level %q of namespace %s: %w", l, ns, err)
		}
		c.targets[ns] = level
	}
	if cluster != "" {
		level, err := api.ParseLevel(cluster)
		if err != nil {
			return nil, fmt.Errorf("cluster target level %q: %w", cluster, err)
		}
		c.cluster = &level
	}
	return c, nil
}

// Check checks the pod against the level of its namespace ns (nil if the
// namespace is not in the snapshot). Only the enforce mode is evaluated,
// because audit and warn never reject a pod (Section 2.9).
func (c *Checker) Check(pod *corev1.Pod, ns *corev1.Namespace) Result {
	lv := c.level(pod.Namespace, ns)
	results := c.evaluator.EvaluatePod(lv, &pod.ObjectMeta, &pod.Spec)
	agg := policy.AggregateCheckResults(results)
	return Result{
		Allowed:    agg.Allowed,
		Level:      lv.String(),
		Violations: agg.ForbiddenDetail(),
	}
}

// level returns the level and version to check a pod in the namespace
// against: a target level if one is set, otherwise the enforce level from
// the namespace labels, read as the API server reads them.
func (c *Checker) level(name string, ns *corev1.Namespace) api.LevelVersion {
	if l, ok := c.targets[name]; ok {
		return api.LevelVersion{Level: l, Version: api.LatestVersion()}
	}
	if c.cluster != nil {
		return api.LevelVersion{Level: *c.cluster, Version: api.LatestVersion()}
	}

	// Without a label the API server applies privileged:latest, the default
	// of its admission configuration.
	defaults := api.Policy{Enforce: api.LevelVersion{Level: api.LevelPrivileged, Version: api.LatestVersion()}}
	var labels map[string]string
	if ns != nil {
		labels = ns.Labels
	}
	// An invalid label falls back to restricted:latest, as in the API server.
	p, _ := api.PolicyToEvaluate(labels, defaults)
	return p.Enforce
}
