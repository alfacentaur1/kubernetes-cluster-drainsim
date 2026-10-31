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

## 2026-10-31 — Interface names without the "I" prefix

**Decision:** Interfaces from the design (Chapter 5) use Go names without the "I" prefix: `ICollector` → `cli.Collector`, `ISimulation` → `engine.Simulation`, `IPlacement` → `Placement`, `IPolicyChecker` → `PolicyChecker`, `ICostModel` → `CostModel`. The GoDoc comment of each interface names the design interface it implements.

**Reason:** The "I" prefix is not idiomatic in Go; interfaces are named after what they do, and the package name already gives context (`engine.Simulation`). The GoDoc reference keeps the code traceable to the design.

**Alternatives:** Keep the exact design names (`ISimulation`), which match the thesis text but look foreign in Go code.

## 2026-10-31 — Sanitization removes all env values

**Decision:** The collector clears the literal `value` of every env var in all containers (incl. init and ephemeral) of pods and pod templates. `valueFrom` is kept.

**Reason:** The design (Section 5.2, NFR12) says the snapshot contains no container env values. A keyword list (PASSWORD, TOKEN, …) only guesses which values are secret and misses names such as `DB_PASS` or `DSN` with an embedded password. The simulation never reads env values, so removing all of them loses nothing. `valueFrom` only references Secrets/ConfigMaps by name.

**Alternatives:** Replace values whose names match a keyword list (the previous implementation); drop the whole `env` list (would also lose `valueFrom`, which may be useful in the report).

## 2026-10-31 — Replacement is a copy of the evicted pod

**Decision:** The replacement model creates a replacement as a pending copy of the evicted pod (no node, no status, no UID), not from the workload's pod template. A ReplicaSet/Job replacement gets the name `<generateName>sim<N>` with a sequence number; a StatefulSet replacement keeps the name.

**Reason:** The evicted pod has already been admitted, so its spec contains everything that admission added (defaults, injected sidecars, …) that the template does not. A sequence number instead of a random suffix keeps the names, and so the whole simulation, deterministic (NFR7).

**Alternatives:** Build the replacement from the template of the owning workload (would need to replay admission); random suffixes as the real controllers use.

## 2026-10-31 — Eviction rules re-implemented from kubectl

**Decision:** `eviction.Decide` re-implements the filters of `k8s.io/kubectl/pkg/drain/filters.go` in the same order: DaemonSet, mirror pod, emptyDir, unreplicated; finished pods are allowed. The orphaned-DaemonSet check is left out.

**Reason:** The kubectl filters call the API server (e.g. to look up the owning DaemonSet), but the simulation runs offline on a snapshot (NFR4). The orphaned-DaemonSet check needs exactly such a lookup.

**Alternatives:** Call the kubectl drain helper with a fake clientset filled from the snapshot (more wiring, behaviour tied to kubectl internals).

## 2026-10-31 — One scheduler framework per simulation

**Decision:** The scheduler adapter creates one framework per simulation. Its `SharedLister` is a wrapper whose snapshot is rebuilt from the cluster state (`cache.NewSnapshot(BoundPods(), Nodes())`) before each placement.

**Reason:** Creating the framework is expensive (all plugins are initialised), while rebuilding the snapshot costs O(p + m) per placement (p pods, m nodes), which is small for the cluster sizes in scope. The plugins always see the current state, including pods placed earlier in the drain.

**Alternatives:** A new framework per placement (slow); updating the scheduler cache incrementally with AddPod/RemovePod (faster, but more code and easy to get out of sync with the cluster state).
