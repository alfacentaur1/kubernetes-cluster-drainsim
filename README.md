# drainsim

A read-only CLI tool that takes a snapshot of a Kubernetes cluster and, **before a change is made**, simulates it offline to answer three questions:

| | Area | Question |
|---|---|---|
| **Q1** | Maintenance | Can all (or selected) nodes be drained one by one? In what order? If not, why? |
| **Q2** | FinOps / GreenOps | How many nodes are actually needed? How much money and CO2 does the surplus cost? |
| **Q3** | DevSecOps | Does a stricter Pod Security level break the next drain? |

One simulation engine, three analyses.

This is a bachelor's thesis project at FEL ČVUT (Czech Technical University in Prague, Faculty of Electrical Engineering). The full design is in [`docs/thesis.md`](docs/thesis.md) (chapter 4 Analysis, chapter 5 Design).

> **Status:** early development — Step 0 (scheduler framework spike in `spike/scheduler/`). The commands below describe the planned interface.

## How it works

1. `drainsim snapshot` reads the cluster (get/list only) and optionally Prometheus, and writes a snapshot file.
2. All other commands work **only with the snapshot file**, with no network access.
3. The simulation replays `kubectl drain` semantics: cordon, evict, respect PodDisruptionBudgets, create replacements by owner kind, check Pod Security, and place pods with the **real kube-scheduler framework plugins** used as a Go library.
4. The result is a report (text or JSON) with an explanation of each failure taken from the simulation event log.

## Planned usage

```sh
# Take a snapshot (the only command that talks to the cluster)
drainsim snapshot -o cluster.json

# Q1: can the nodes be drained, and in what order?
drainsim drain cluster.json

# Q2: how many nodes are needed, and what does the surplus cost?
drainsim consolidate cluster.json

# Q3: does a stricter Pod Security level break the next drain?
drainsim policy cluster.json
```

Exit codes: `0` success, `1` the analysed change would fail, `2` usage or input error.

## Guarantees

- **Read-only.** Only `get`/`list` on the Kubernetes API. Nothing is ever created, updated, deleted, evicted or bound in a real cluster.
- **Offline.** Only `snapshot` makes network calls; the analyses run from the snapshot file alone.
- **Real scheduler.** Pod placement uses the upstream kube-scheduler framework plugins (PreFilter, Filter, PreScore, Score, NormalizeScore). Scheduling logic is not re-implemented.
- **Deterministic.** The same snapshot and configuration always give the same result.
- **No secrets.** The snapshot contains no Secrets, no ConfigMaps, no container env values and no managedFields.
- **Upstream Pod Security.** Checks use the `k8s.io/pod-security-admission` library.

## Simulation model

A drain step cordons the node and handles each pod:

- **Eviction rules** decide per pod: *evict*, *skip* (DaemonSet, mirror pod) or *refuse* (no controller, emptyDir).
- For an evicted pod: **PDB check → replacement by owner kind → PSA check** (if target levels are set) **→ scheduler places it → state update**.
- The PDB budget is restored only after the replacement is placed; a placed replacement is assumed Ready.
- A replacement rejected by PSA is a lost replica; a replacement with no suitable node stays Pending.
- Evictions that are not allowed yet are retried in the next round. A round without progress means the drain fails, and the cause comes from the event log.
- A pod covered by more than one PDB is never evictable (the API server returns 500).

## Project layout

| Package | Responsibility |
|---|---|
| `internal/cli` | Commands: `snapshot`, `drain`, `consolidate`, `policy` |
| `internal/report` | Reporter: text, JSON, exit codes |
| `internal/analysis/drainorder` | Q1: drain order search (greedy + backtracking) |
| `internal/analysis/consolidation` | Q2: consolidation and right-sizing |
| `internal/analysis/policyimpact` | Q3: two simulations, without and with target levels |
| `internal/cost` | Cost and CO2 estimator |
| `internal/engine` | Simulation engine facade |
| `internal/engine/state` | Cluster state with save/restore |
| `internal/engine/eviction` | Eviction rules (`kubectl drain` semantics) |
| `internal/engine/pdb` | PDB model (budget, recovery, `unhealthyPodEvictionPolicy`, multiple PDBs) |
| `internal/engine/replacement` | Replacement model by workload kind |
| `internal/engine/scheduler` | Adapter over the kube-scheduler framework |
| `internal/engine/psa` | Pod Security checker over `pod-security-admission` |
| `internal/engine/events` | Event log, source of failure explanations |
| `internal/snapshot` | Snapshot model, loader and collector (Kubernetes API, Prometheus) |
| `internal/config` | Configuration: prices, CO2 coefficients, policy targets |
| `test/testdata/` | Small snapshots for unit tests |
| `test/scenarios/<id>/` | Reproducible test clusters (`kind.yaml`, `manifests/`, `expected.json`) |
| `docs/` | Thesis text and decision log |

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
