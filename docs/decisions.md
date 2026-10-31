# Decisions

## 2026-10-27 — Pin Kubernetes libraries to v1.36.5

**Decision:** Depend on `k8s.io/kubernetes v1.36.5` and replace all 33 staging modules (`k8s.io/api`, `k8s.io/client-go`, …) with `v0.36.5` in `go.mod`. Scenario clusters use the matching `kindest/node:v1.36.x` image.

**Reason:** The scheduler plugins live only in `k8s.io/kubernetes`, whose `go.mod` requires the staging modules at `v0.0.0` with local `replace` directives, so it cannot be imported without replacing them. Cluster Autoscaler does the same. 1.36 is a mature minor; 1.37 was very fresh.

**Alternatives:** `k8s.io/kube-scheduler` alone (only interfaces, no plugins); re-implementing the plugins (violates NFR5); v1.37.x.

## 2026-10-27 — Scheduler framework wiring (Step 0 spike)

**Decision:** Build the framework the way Cluster Autoscaler does: `plugins.NewInTreeRegistry()` + the default profile from `latest.Default()` + `cache.NewSnapshot(pods, nodes)` as the `SharedLister`, with a fake clientset and an unstarted informer factory. Run PreFilter → Filter (per node) → PreScore → Score (incl. NormalizeScore) by hand. Call `schedulermetrics.InitMetrics()` first, otherwise `NewFramework` panics on nil metric vectors.

**Reason:** No API server is needed (NFR3, NFR4), and placement uses the upstream plugins (NFR5). Nodes are evaluated in name order and score ties go to the lowest node name (NFR7).

**Alternatives:** Running the full `scheduler.Scheduler` with a fake API server (heavier, async, binds pods).

## 2026-10-27 — DynamicResources (DRA) plugin disabled

**Decision:** Remove `DynamicResources` from the profile's MultiPoint plugins.

**Reason:** The plugin dereferences `SharedDRAManager` in every PreFilter and panics without one. The snapshot collects no ResourceClaims/DeviceClasses, so the plugin could not make meaningful decisions anyway. This is a deviation from "all default plugins" and should be stated in the thesis scope.

**Alternatives:** Provide an empty or snapshot-backed `SharedDRAManager` (as Cluster Autoscaler does), which would be needed if DRA ever becomes in scope.

## 2026-10-27 — One rejection reason per node

**Decision:** For an unschedulable pod, report the status of the first Filter plugin that rejects each node (what `RunFilterPlugins` returns).

**Reason:** This matches what kube-scheduler itself reports in the `FailedScheduling` event.

**Alternatives:** Run each Filter plugin separately per node to collect all reasons (e.g. a node that is both tainted and too small). To be revisited if the explanations in Q1 need it.
