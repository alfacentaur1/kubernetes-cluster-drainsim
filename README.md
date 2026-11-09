# drainsim

A read-only CLI tool that takes a snapshot of a Kubernetes cluster and, **before a change is made**, simulates it offline to answer three questions:

| | Area | Question |
|---|---|---|
| **Q1** | Maintenance | Can all (or selected) nodes be drained one by one? In what order? If not, why? |
| **Q2** | FinOps / GreenOps | How many nodes are actually needed? How much money and CO2 does the surplus cost? |
| **Q3** | DevSecOps | Does a stricter Pod Security level break the next drain? |

One simulation engine, three analyses.

This is a bachelor's thesis project at FEL ČVUT (Czech Technical University in Prague, Faculty of Electrical Engineering). The design is described in the bachelor's thesis (chapter 5); design decisions taken during the implementation are recorded in [`docs/decisions.md`](docs/decisions.md).

> **Status:** the `snapshot` command and the complete simulation engine (cluster state, eviction rules, PDB model, replacement model, PSA checker, scheduler adapter, event log) are implemented. The `drain`, `consolidate` and `policy` commands and the report are in progress; the usage below shows the planned interface.

## How it works

1. `drainsim snapshot` reads the cluster (get/list only) and optionally Prometheus, and writes a snapshot file.
2. All other commands work **only with the snapshot file**, with no network access.
3. The simulation replays `kubectl drain` semantics: cordon, evict, respect PodDisruptionBudgets, create replacements by owner kind, check Pod Security, and place pods with the **real kube-scheduler framework plugins** used as a Go library.
4. The result is a report (text or JSON) with an explanation of each failure taken from the simulation event log.

## Planned usage

```sh
# Take a snapshot (the only command that talks to the cluster)
drainsim snapshot -o cluster.json

# ... optionally with the usage of the workloads from Prometheus (FR20)
drainsim snapshot -o cluster.json --prometheus http://localhost:9090 --usage-window 7d

# Q1: can the nodes be drained, and in what order?
drainsim drain cluster.json

# Q2: how many nodes are needed, and what does the surplus cost?
drainsim consolidate cluster.json

# Q3: does a stricter Pod Security level break the next drain?
drainsim policy cluster.json
```

### Usage profiles from Prometheus

`drainsim snapshot` reads the usage of the workloads only when `--prometheus`
is given; without it the snapshot has no usage profiles, and the consolidation
works with the requests alone.

| Flag | Default | Meaning |
|---|---|---|
| `--prometheus URL` | – | Base URL of the Prometheus HTTP API, e.g. `http://localhost:9090` (`kubectl port-forward`) |
| `--usage-window W` | `7d` | Length of the history, e.g. `7d`, `24h`, `90m` (at least `5m`); requires `--prometheus` |

For every container, the snapshot stores the 95th percentile of CPU usage and
the maximum working-set memory over the window, assigned to its workload
(Deployment, StatefulSet, DaemonSet, Job, or the ReplicaSet/Pod itself when
nothing owns it) through `kube_pod_owner`. A workload gets the maximum over
its pods. Prometheus must scrape cAdvisor (`container_*` metrics) and
kube-state-metrics. A failed query fails the snapshot (exit code `2`).

Exit codes: `0` success, `1` the analysed change would fail, `2` usage or input error.

## Guarantees

- **Read-only.** Only `get`/`list` on the Kubernetes API (see `test/rbac/readonly.yaml`). Nothing is ever created, updated, deleted, evicted or bound in a real cluster. Prometheus is only queried through `GET /api/v1/query`, so read access to its query API is enough; no Kubernetes permission is needed for it.
- **Offline.** Only `snapshot` makes network calls; the analyses run from the snapshot file alone.
- **Real scheduler.** Pod placement uses the upstream kube-scheduler framework plugins (PreFilter, Filter, PreScore, Score, NormalizeScore). Scheduling logic is not re-implemented.
- **Deterministic.** The same snapshot and configuration always give the same result.
- **No secrets.** The snapshot contains no Secrets, no ConfigMaps, no container env values and no managedFields.
- **Upstream Pod Security.** Checks use the `k8s.io/pod-security-admission` library.

## Simulation model

A drain step cordons the node and handles each pod:

- **Eviction rules** decide per pod: *evict*, *skip* (DaemonSet, mirror pod) or *refuse* (no controller, emptyDir).
- For an evicted pod: **PDB check → replacement by owner kind → PSA check** (against the enforce level of the namespace, or a target level when one is configured) **→ scheduler places it → state update**.
- The PDB budget is restored only after the replacement is placed; a placed replacement is assumed Ready.
- A replacement rejected by PSA is a lost replica; a replacement with no suitable node stays Pending.
- Evictions that are not allowed yet are retried in the next round. A round without progress means the drain fails, and the cause comes from the event log.
- A pod covered by more than one PDB is never evictable (the API server returns 500).
- A drain fails while a refused pod is left on the node, as kubectl drain does without --force.

## Repository layout

| Package | Component |
|---|---|
| `internal/cli` | Commands of the CLI (`snapshot`; `drain`, `consolidate`, `policy` planned) |
| `internal/reporter` | Reporter: exit codes |
| `internal/collector` | Collector: reads a live cluster into a snapshot |
| `internal/snapshot` | Snapshot artifact, stored as versioned JSON |
| `internal/engine` | Simulation engine (Facade) |
| `internal/engine/loader` | Snapshot loader: reads a snapshot file into the cluster state |
| `internal/engine/state` | Cluster state (Memento) |
| `internal/engine/eviction` | Eviction rules (`kubectl drain` semantics) |
| `internal/engine/pdb` | PDB model |
| `internal/engine/replacement` | Replacement model |
| `internal/engine/scheduler` | Scheduler adapter over the kube-scheduler framework |
| `internal/engine/psa` | PSA checker over `pod-security-admission` (Adapter) |
| `internal/engine/eventlog` | Event log (Observer) |

## Test scenarios

Each scenario from the Analysis chapter (4.1) has a reproducible kind cluster in `test/scenarios/<id>/`.

| ID | Scenario |
|---|---|
| P1 | PDB allows no eviction |
| P2 | No node has enough capacity |
| P3 | Pod's rules exclude every node |
| P4 | Order of drains matters |
| P5 | Drain does not say why |
| P7 | Drain uses up the budget |
| P8 | Broken pods use up the budget |
| P9 | Drain refuses pods |
| P10 | Pod in two PDBs |
| F1–F3, G1 | Consolidation and savings |
| S1–S3 | Policy impact |

## Development

```sh
go test ./...
```

Unit tests run against small snapshots in `test/testdata/` and need no cluster. Scenario tests use [kind](https://kind.sigs.k8s.io/) with a node image that matches the pinned `k8s.io/*` library version.

Design decisions, library choices and deviations from the thesis are recorded in [`docs/decisions.md`](docs/decisions.md).

## Out of scope

Volume topology, priorities and preemption, drain duration, autoscalers, measured energy, Kyverno / Gatekeeper / ValidatingAdmissionPolicy, ResourceQuota, platform-specific features, and executing any change.
