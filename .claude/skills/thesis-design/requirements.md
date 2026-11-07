# Requirements (thesis Section 4)

Priority: M = must, S = should, C = could.

## Snapshot
- **FR1** Read cluster state (M, UC1): nodes, pods, owners, PDBs, namespace labels from the Kubernetes API.
- **FR2** Save snapshot (M, UC1): one file that can be shared and reused.
- **FR3** Load snapshot (M, UC2): build the in-memory cluster state from it.
- **FR4** Validate snapshot (S, UC2): check format and version, clear error.

## Drain simulation
- **FR5** Select nodes (M, UC3, UC4): all, by name, or by label.
- **FR6** Cordon node (M): mark unschedulable before draining.
- **FR7** Evict pods (M): same rules as the Kubernetes Eviction API.
- **FR8** Place evicted pods (M): kube-scheduler Filter and Score plugins.
- **FR9** Update state (M): where pods run and how much capacity is left.
- **FR10** Check PDBs (M): before each eviction.
- **FR11** Recover PDB budget (M): only after the replacement is scheduled.
- **FR12** Unhealthy pods (M): follow unhealthyPodEvictionPolicy.
- **FR13** Multiple PDBs (M): such a pod is not evictable.
- **FR14** Skip DaemonSet pods (M, UC7).
- **FR15** Refused pods (M, UC7): no controller or emptyDir.

## Drain order (Q1)
- **FR16** Search drain order (M, UC5): try other orders if one fails.
- **FR17** Return drain order (M, UC5).
- **FR18** Report failure cause (M, UC6): blocking pod, PDB or rule, node, step.
- **FR19** Report scheduler reason (S, UC6): plugin that rejected each node.

## Consolidation, FinOps, GreenOps (Q2)
- **FR20** Read usage data (S, UC8): container CPU/memory history from Prometheus, assigned to workloads.
- **FR21** Right-size CPU (S, UC9): 95th percentile of usage.
- **FR22** Right-size memory (S, UC9): maximum usage plus a buffer.
- **FR23** Re-simulate (S, UC9): with right-sized requests, show new node count.
- **FR24** Find removable nodes (S, UC10): while all pods still run; report a lower bound.
- **FR25** Verify removal (S, UC10): simulated as a drain (PDBs, order).
- **FR26** Load node prices (S, UC11): from a configuration file.
- **FR27** Cost savings (S, UC11): money saved per month.
- **FR28** CO₂ savings (C, UC12): SCI formula, CCF energy coefficients.

## Policy impact (Q3)
- **FR29** Set target policy (S, UC13): Pod Security level per namespace or cluster.
- **FR30** Evaluate pods (S, UC13): every pod against the target level.
- **FR31** Lost replica (S, UC14): rejected replacement is lost.
- **FR32** Policy impact on drain (S, UC14): counts against its PDB, affects later steps.

## Output
- **FR33** Text report (M). **FR34** JSON report (M, UC15).
- **FR35** Exit codes (M, UC16): 0 success, non-zero failed check, separate code for errors.
- **FR36** Non-interactive mode (M, UC16).

## Non-functional
- **NFR1** Read-only access (M). **NFR2** No cluster changes (M).
- **NFR3** Offline simulation (M). **NFR4** No network during simulation (M).
- **NFR5** Real scheduler (M): upstream kube-scheduler code.
- **NFR6** Agreement with real drains (M): verified in RQ1 on kind.
- **NFR7** Deterministic result (M). **NFR8** Versioned snapshot (S).
- **NFR9** 100 nodes / 3,000 pods in under 60 s (S, to confirm). **NFR10** < 1 GB memory (C).
- **NFR11** Data stays local (M). **NFR12** No secrets in snapshot (M).
- **NFR13** Any conformant distribution (M). **NFR14** Supported versions documented (S).
- **NFR15** Single binary (S). **NFR16** Plain explanations (S). **NFR17** Consistent CLI (S).
- **NFR18** Pluggable policy checks (C). **NFR19** Pluggable cost data (C).
- **NFR20** Test cluster per scenario (M). **NFR21** Automated tests (S). **NFR22** Open source (S).

## Scenarios referenced in code
P1 budget blocks eviction · P2/P3 replacement fits nowhere · P4 order matters ·
P7 pending replacement keeps budget reduced · P8 unhealthy pods · P9 refused pods ·
P10 pod under several PDBs · S1 running pods violate a new level ·
S2 lost replica · S3 report the violated control · F1 right-sizing.
The full list is in the thesis (Chapter 4); ask before relying on one not listed.
