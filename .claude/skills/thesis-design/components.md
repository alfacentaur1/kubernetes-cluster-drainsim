# Components, patterns and algorithms (thesis Chapter 5)

| Component (Fig 5.1 / 5.3) | Package | Pattern | Section |
|---|---|---|---|
| CLI | `internal/cli` | Command | 5.7 |
| Reporter | `internal/reporter` | – | 5.7 |
| Snapshot collector | `internal/collector` | – | 5.2 |
| Snapshot / data model (Fig 5.2) | `internal/snapshot` | – | 5.2 |
| Configuration | `internal/config` | – | 5.2, 5.5, 5.6 |
| Simulation engine (ISimulation) | `internal/engine` | Facade | 5.3 |
| Snapshot loader | `internal/engine/loader` | – | 5.3 |
| Cluster state | `internal/engine/state` | Memento | 5.3 |
| Eviction rules | `internal/engine/eviction` | – | 5.3, 2.7 |
| PDB model | `internal/engine/pdb` | – | 5.3, 2.8 |
| Replacement model | `internal/engine/replacement` | Factory Method | 5.3 |
| Scheduler adapter (IPlacement) | `internal/engine/scheduler` | Adapter | 5.3, 2.6 |
| PSA checker (IPolicyChecker) | `internal/engine/psa` | Adapter, Strategy | 5.3, 2.9 |
| Event log | `internal/engine/eventlog` | Observer | 5.3 |
| Drain order search (Q1) | `internal/analyses/drainorder` | Strategy | 5.4 |
| Consolidation (Q2), ICostModel | `internal/analyses/consolidation` | Strategy | 5.5 |
| Policy impact (Q3) | `internal/analyses/policyimpact` | – | 5.6 |
| Cost and CO₂ estimator | `internal/estimator` | – | 5.5 |

## One drain step (Figure 5.4)

```
cordon node
repeat rounds over the pods on the node (sorted by namespace, name):
    eviction rules: skip (DaemonSet, mirror) | refuse (no controller, emptyDir) | evict
    PDB model: not allowed → defer to next round (also for several PDBs)
    evict the pod (budget − 1 if Ready)
    replacement by owner kind (ReplicaSet/Job: new name, StatefulSet: same name)
    PSA check: rejected → lost replica (budget stays reduced)
    scheduler (PreFilter, Filter, PreScore, Score): no node → Pending
    bind, mark Ready, recover budget (+1)
until no deferred pods (drained) or a round without progress (fails)
refused pods left → drain fails
```

## Algorithms

- **5.1 Greedy (Q1):** at each step try remaining nodes by requests of the pods
  that would move (CPU, memory, name); take the first that drains; never go back.
- **5.2 Backtracking (Q1):** like greedy, but undo a successful drain when no
  complete order follows; optional limit of attempts; remember the longest
  sequence for the report.
- **5.3 Consolidation (Q2):** sort once by requests of all pods on the node;
  try each: save, drain, remove the node if the drain passes and all
  replacements are placed, else restore. Report a lower bound.
- **5.4 Policy impact (Q3):** check running pods against targets; save; drain
  with current levels; restore; drain with target levels; a step that passes
  before and fails after is the impact; report lost replicas and violations.

## Fixed interfaces (Go names, design names in brackets)

`engine.Simulation` (ISimulation) · `engine.Placement` (IPlacement) ·
`engine.PolicyChecker` (IPolicyChecker) · `consolidation.CostModel` (ICostModel) ·
`cli.Collector` (ICollector). An interface lives in the package that uses it.
When `engine.Simulation` grows, update `fakeSim` in
`internal/analyses/drainorder/drainorder_test.go`.
