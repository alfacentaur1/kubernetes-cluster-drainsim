# Scenario: basic

The first reproducible scenario test (NFR20, RQ1). It compares the simulated
drain of drainsim with a real `kubectl drain` on a kind cluster.

## What it shows

- **Read-only access is enough (NFR1).** The snapshot is taken with a
  kubeconfig that holds only the token of the service account
  `drainsim-collector` from `test/rbac/readonly.yaml`. The script checks that
  this account can neither evict pods nor patch nodes.
- **Same answer as the real drain.** The cluster has one control plane
  (tainted, so it takes no workload) and two workers. Deployment `web` has
  three replicas with a PodDisruptionBudget `minAvailable: 2`, so one
  disruption is allowed at a time.
  - Draining worker 1 succeeds: the replicas move to worker 2 one at a time,
    each replacement restoring the budget.
  - Draining worker 1 and then worker 2 fails: the first replica evicted from
    worker 2 has nowhere to go (worker 1 is cordoned, the control plane is
    tainted), so its replacement stays Pending, the budget never recovers and
    the other replicas stay. `kubectl drain` waits until its timeout; drainsim
    reports the failure and the pods that blocked it.

| case | drainsim | kubectl drain |
|---|---|---|
| worker 1 | drained, exit 0 | drained, exit 0 |
| worker 1, then worker 2 | worker 2 fails, exit 1 | worker 2 times out, exit 1, one pod Pending |

The placement of the three replicas at the start is chosen by the real
scheduler and may differ between runs (all on worker 1, or split); the
results above hold in both cases.

