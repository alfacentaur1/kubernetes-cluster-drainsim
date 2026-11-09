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

## 2026-11-04 — PDB model follows the Eviction API

**Decision:** The PDB model re-implements the checks of the Eviction API
(pkg/registry/core/pod/storage/eviction.go, Kubernetes 1.36) in the same
order. The starting values of currentHealthy, desiredHealthy and
disruptionsAllowed are taken from the PDB status in the snapshot. The check
of observedGeneration is left out.

**Reason:** The status was computed by the disruption controller of the real
cluster, so the tool does not have to compute desiredHealthy from
minAvailable, maxUnavailable and the scale of the owner again. The Eviction
API itself cannot be called offline (NFR4). A snapshot is one moment, so a
budget whose status lags behind its spec is not expected.

**Alternatives:** Computing the status from the spec and the replica counts,
as the disruption controller does.

## 2026-11-04 — PSA checker uses the Pod Security Admission library

**Decision:** The PSA checker evaluates pods with `k8s.io/pod-security-admission`
(`policy.DefaultChecks`, `api.PolicyToEvaluate`). Only the enforce mode is
evaluated. A namespace without a label is checked against `privileged:latest`,
and an invalid label falls back to `restricted:latest`, as in the API server.
A target level from the configuration replaces the level of the namespace;
a per-namespace target comes before a cluster-wide one (FR29). Exemptions from
the admission configuration of the API server are not modelled.

**Reason:** The library is the code that runs inside the API server, so the
result matches a real admission check (S1, NFR5). Audit and warn never reject
a pod (Section 2.9). Exemptions are not part of the snapshot, and the default
admission configuration has none.

**Alternatives:** Re-implementing the Pod Security Standards (error-prone and
would drift from upstream); reading the admission configuration from the
control plane (not available through the API).

## 2026-11-04 — Refused pods fail the drain

**Decision:** The engine reports the drain of a node as failed if any pod on it
was refused by the eviction rules, even when all other pods were evicted. This
adds a branch to Figure 5.4 before "Node drained".

**Reason:** kubectl drain without --force and --delete-emptydir-data stops at
pods without a controller and pods with an emptyDir volume (Section 2.7, P9).
Without this rule the simulation would report a drain as successful that fails
in reality.

**Alternatives:** Report refused pods only as warnings (contradicts kubectl
drain).

## 2026-11-04 — Checkpoint includes the replacement counter

**Decision:** `engine.Checkpoint` stores, besides the Memento of the cluster
state, the counter the replacement model uses to name replacements.
`Restore` sets it back as well.

**Reason:** The policy impact analysis runs the same drain twice from the same
saved state and compares the runs step by step (Section 5.6). Without the
counter, the second run would give replacements other names and the steps
could not be matched. It also keeps repeated simulations deterministic (NFR7).

**Alternatives:** Deriving replacement names from the evicted pod only (names
could collide when the same pod is replaced twice).

## 2026-11-04 — Command line of the analysis commands

**Decision:** The analysis commands take the snapshot file as a positional
argument (`drainsim drain cluster.json`). The report format is chosen with
`--format text|json` (default text). Nodes are selected with
`--nodes a,b,c` (in the given order) or `--selector key=value` (sorted by
name); without either, all nodes are used, sorted by name (FR5).

**Reason:** A positional argument keeps the commands short and matches the
README. `-o` already means the output file of `snapshot`, so using it for the
format would give one flag two meanings (NFR17). Sorting by name keeps the
result deterministic when no order is given (NFR7).

**Alternatives:** `--snapshot` flag; `-o json` as in kubectl.

## 2026-11-04 — Format of the configuration file

**Decision:** The configuration is a YAML file passed with `--config`. The
target Pod Security levels are under `policyTargets`: `cluster` for every
namespace and `namespaces` for single namespaces, which take precedence
(FR29). Unknown fields are rejected.

**Reason:** YAML is the format Kubernetes operators use daily. The two keys
map directly to the arguments of the PSA checker. Rejecting unknown fields
prevents a typo from silently disabling the target levels, which would make
the analysis report that the new policy has no effect.

**Alternatives:** JSON (harder to write by hand); command-line flags such as
`--target shop=restricted` (unwieldy for many namespaces and for the prices
added later).

## 2026-11-07 — Consolidation keeps a node unless all pods still run

**Decision:** In Algorithm 5.3, the drain of a node counts as a success only
if the drain passes and every replacement is placed. A removed node is deleted
from the cluster state together with the DaemonSet and mirror pods left on it.
The lower bound uses the largest node and the larger of the CPU and memory
bounds.

**Reason:** FR24 asks for nodes that can be removed while all pods still run,
but a drain passes even when a replacement stays Pending (Section 2.7).
DaemonSet and mirror pods exist only with their node. Using the largest node
keeps the bound valid when nodes differ in size, and equals the formula of
Section 5.5 for nodes of one size.

**Alternatives:** Accepting a drain with pending replacements (would report
nodes as removable while pods stop running); the average node size for the
bound (not a valid lower bound for mixed nodes).

## 2026-11-07 — Cost and CO₂ estimate

**Decision:** The estimate covers a period of 730 h (one month, 8760 h / 12)
unless the configuration sets `hours`. The number of vCPUs and the memory of
a node come from its Capacity, not its Allocatable; 1 GB is 10^9 bytes. Only
the idle power of the vCPUs and the energy of the memory are saved, plus the
embodied emissions of the machine. Money and CO₂ are known separately: a node
with a price but no grid intensity for its region has a known money saving
and an unknown CO₂ saving; a node whose instance type is not configured is
still removed, with both savings unknown.

**Reason:** FR27 asks for a monthly saving, and 730 h is the average month
used by cloud price lists. The whole machine draws power, including the part
reserved for the system, so Capacity describes the saved energy. The
coefficients of Cloud Carbon Footprint are given per 10^9 bytes. The pods of
a removed node keep running elsewhere, so only the idle power goes away.
Prices and grid intensities come from different sources, and a missing one
should not hide the other.

**Alternatives:** Allocatable resources (underestimate the saving); GiB
(inconsistent with the coefficients); a single "known" flag (would drop the
money saving whenever the region is missing); skipping nodes without a price
(would change the answer to Q2 because of missing cost data).

## 2026-11-07 — Scenario tests compare the outcome, not the timing

**Decision:** The kind scenarios (`test/scenarios/`) compare, per node,
whether the drain succeeds and where the pods end up. They do not compare
how long a drain takes or the exit code of `kubectl drain` with that of
drainsim. drainsim reports a blocked drain as soon as a round of evictions
makes no progress; `kubectl drain` retries every 5 s until its `--timeout`.
The node image is `kindest/node:v1.36.4`, the newest 1.36 image of kind
v0.33.0, while the libraries are pinned to v1.36.5.

**Reason:** A budget that cannot recover (its replacement stays Pending)
never lets the drain finish, so waiting longer cannot change the answer;
the timeout of kubectl only decides when it gives up. Exit code 1 means a
failed check for drainsim but any error for kubectl, so equal codes would be
a coincidence. Patch versions of one minor release do not change the
scheduler or the Eviction API that the simulation re-implements.

**Alternatives:** Simulating the kubectl timeout (adds time to a model that
has none, NFR7); building a 1.36.5 node image ourselves (slower, not
reproducible from the kind release notes).

## 2026-11-07 — The consolidate command always exits with 0

**Decision:** `drainsim consolidate` returns exit code 0 for every run that
completes, also when no node can be removed, and 2 for invalid input (bad
flag, unreadable snapshot or configuration, unknown node). It never returns
1. Its report always has `passed: true`. Without `--config`, or with a
configuration that has no instance types in `estimate`, it runs without a
cost model and reports the savings as unknown.

**Reason:** Exit code 1 means "the check failed" (FR35): the given nodes
cannot be drained, or a policy change would break a drain. The consolidation
answers a question (Q2) instead of checking a condition: "no node can be
removed" is a valid answer, not a failure, and a pipeline should not stop on
it. Prices and emission factors are the user's decision and often
unavailable, so the removable nodes must be found without them (Section 5.5).

**Alternatives:** Exit code 1 when no node can be removed (would mark a
well-sized cluster as an error); requiring `--config` (would hide the answer
to Q2 when no prices are at hand).

## 2026-11-09 — Usage profiles are optional and keep the snapshot format version

**Decision:** The snapshot gets an optional field `usageProfiles` (omitted
when empty) holding one profile per workload: namespace, kind, name, the
window, and per container the 95th percentile of CPU in millicores and the
maximum working-set memory in bytes. `formatVersion` stays
`drainsim.snapshot/v1`. The profiles are kept in the cluster state, outside
the Memento, and returned as copies by `engine.Simulation.UsageProfiles`.

**Reason:** Usage profiles are part of the snapshot data model (Figure 5.2)
and are read from Prometheus (FR20), but Prometheus is optional: the
consolidation works without them (Section 5.5). Adding an optional field
leaves every older snapshot valid, so no version change is needed (FR4,
NFR8). The profiles describe the measured past, which a simulated drain does
not change.

**Alternatives:** `drainsim.snapshot/v2` (would reject valid older snapshots
for no gain); a separate usage file (a second artifact the analyses would
have to keep in sync with the snapshot).

## 2026-11-09 — A workload gets the maximum usage over its pods

**Decision:** The collector assigns the usage of each container to the
workload of its pod: the controller from `kube_pod_owner`, a ReplicaSet
resolved to its Deployment with the ReplicaSets of the snapshot (the rule of
`state.Workload`), and a pod without an owner as its own workload of kind
`Pod`. When a workload has several pods (replicas, or pods replaced during
the window), each container gets the maximum over the pods, both for CPU
p95 and memory. Values are rounded up to whole millicores and bytes;
samples that are NaN or infinite are skipped.

**Reason:** The right-sizing sets one request for all pods of a workload
(FR21, FR22), because they share their template. Taking the maximum means a
right-sized request covers every replica, including the busiest one, so the
simulation never assumes less than any pod used. Rounding up keeps this
property. `max_over_time(kube_pod_owner[W])` keeps pods that no longer
exist, so a rollout during the window does not lose usage.

**Alternatives:** The mean or median over pods (would under-size the
busiest replica and could hide a placement failure); a percentile over all
samples of all pods pooled together (a heavier query with no clear gain for
a conservative estimate).

## 2026-11-09 — Usage queries are grouped by pod and use only controllers

**Decision:** Both Prometheus queries group by `namespace, pod, owner_kind,
owner_name, container` instead of only by the owner, and the maximum over
the pods is taken in Go. `kube_pod_owner` is filtered with
`owner_is_controller!="false"`. The queries are instant queries evaluated at
the time of the snapshot (`time=createdAt`). The window is written in the
syntax of Prometheus with the largest whole unit (`7d` → `168h`) and stored
in the profile; `--usage-window` accepts Go durations and whole days (`7d`),
at least `5m`.

**Reason:** Pods without an owner all have `owner_name="<none>"`; grouping
by the owner alone would merge them into one series and lose the pod name
that identifies them as workloads of kind `Pod`. The ReplicaSet → Deployment
step needs the snapshot anyway, so the last aggregation is done in Go. Only
the controller owns a pod in the sense of `metav1.GetControllerOf`, which the
rest of drainsim uses. Evaluating at `createdAt` makes the usage describe the
same moment as the objects.

**Alternatives:** Grouping by owner only, as first sketched (loses ownerless
pods); a separate query for ownerless pods (a third query for a rare case).
