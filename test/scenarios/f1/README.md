# Scenario: F1 — right-sizing

Scenario F1 of the thesis (consolidation after right-sizing): requests that
are much higher than the real usage keep nodes that are not needed. The test
covers FR20 (usage from Prometheus), FR21/FR22 (right-sized CPU and memory),
FR23 (consolidation with the right-sized requests), NFR1 (read-only access,
also to Prometheus) and NFR20 (a test cluster per scenario).

## What it shows

The cluster has one control plane and three workers. Every worker reserves 14
of the 16 CPUs of the development machine (`system-reserved`), so each has
**2 CPU allocatable**. The monitoring stack runs on the control plane, so the
workers hold only the workloads of the scenario.

| Deployment | replicas | requests | real usage |
|---|---|---|---|
| `over-a` | 3 | 800m, 512Mi | ~50m (busy 5 % of each second), ~70Mi |
| `over-b` | 3 | 800m, 512Mi | ~50m, ~70Mi |
| `hungry` | 2 | 100m, 64Mi | one full core, ~110Mi |

- **With the current requests no worker can be removed.** Two 800m pods fill a
  worker, so the six pods need all three. drainsim keeps every worker; the
  real drain of one worker leaves pods Pending.
- **Right-sized, one worker can be removed.** The over-requested pods get
  about 50m, and the hungry pods are raised to about one core (flag
  *higher*, Section 5.5). Two hungry pods do not fit on one worker, so the
  right-sized pods need two workers, not one. drainsim reports one removable
  worker. The script then applies the recommended requests with
  `kubectl set resources`, waits for the rollouts and drains that worker for
  real. All pods must stay Running.
- **Usage comes from real metrics.** The collector reads cAdvisor
  (`container_cpu_usage_seconds_total`, `container_memory_working_set_bytes`)
  and kube-state-metrics (`kube_pod_owner`). The pods are owned by
  ReplicaSets, which the collector resolves to their Deployments. The script
  prints the profile next to usage observed with a separate query.
- **Prometheus needs only read access (NFR1).** drainsim sends only
  `GET /api/v1/query`. The admin API of Prometheus, its only part that
  writes, is disabled, and the script checks that it refuses a request. The
  read-only service account of the snapshot cannot port-forward; the
  port-forward is made with the admin kubeconfig of the scenario, like an
  operator would.

The usage window is 15 minutes (`--usage-window 15m`), so the scenario takes
about 25 minutes, not days. The script lets the workloads run for 16 minutes
(`SETTLE=960`) before the snapshot.

## Versions

| Component | Version |
|---|---|
| kind | v0.33.0 |
| node image | `kindest/node:v1.36.4` (digest in `kind.yaml`) |
| Helm | v3.22.1 |
| kube-prometheus-stack chart | **92.13.1** (2026-10-24), values in `prometheus-values.yaml` |
| workload image | `python:3.13-alpine` |

## How to run

Requirements: Docker, kind, kubectl, jq, Helm 3 and curl on Linux or WSL 2.

```bash
./test/scenarios/f1/run.sh
```

As in `test/scenarios/basic`, `DRAINSIM` can point to a Linux binary that
was cross-compiled on Windows. The script uses its own kubeconfig in `out/`
and deletes the cluster `drainsim-f1` at the end, unless `KEEP=1` is set.
Everything it produces goes to `out/`: the snapshot, the text and JSON
reports, the raw Prometheus responses, the drain logs and where the pods
ended up.

## Last run (2026-11-09)

Usage profiles against usage observed with a separate query (mean CPU over
10 min, maximum working set, busiest pod of the workload):

| workload | CPU p95 (profile) | observed | recommended | memory max (profile) | observed | recommended |
|---|---|---|---|---|---|---|
| Deployment/hungry | 1000m | 984m | 1000m (*higher*) | 103Mi | 103Mi | 120Mi |
| Deployment/over-a | 51m | 49m | 51m | 63Mi | 63Mi | 74Mi |
| Deployment/over-b | 51m | 49m | 51m | 63Mi | 63Mi | 74Mi |

| case | drainsim | real | agree |
|---|---|---|---|
| current requests, drain of worker2 | kept (replacement would stay Pending) | drain exits 0, `over-a` replacement Pending | yes |
| right-sized, drain of worker2 | removed | drain exits 0, all pods Running on worker and worker3 | yes |

drainsim summary: *With the current requests 0 of 3 nodes can be removed;
with right-sized requests 1 (at least 2 nodes are needed).* The read-only
account could not evict, patch nodes or port-forward; the admin API of
Prometheus answered HTTP 500 (disabled).

Found by this scenario and fixed in drainsim (see `docs/decisions.md`):

- Running pods report their requests also in `status.containerStatuses`,
  which the scheduler counts since in-place pod resize. Right-sizing changed
  only the spec, so no node could be removed; it now changes the status too.
- The JSON report wrote empty lists as `null`; they are now `[]`.

The collector needed no change: cAdvisor reports the pause container
without a `container` label (dropped by `container!=""`), and the extra
labels of `kube_pod_owner` (`container`, `uid`, `instance`) are dropped by
the aggregation before the join.
