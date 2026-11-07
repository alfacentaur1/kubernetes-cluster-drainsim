---
name: kind-scenario
description: Create or run a reproducible scenario test on a local kind cluster that compares drainsim with a real kubectl drain (NFR6, NFR20, RQ1). Use when the user wants to test drainsim on a real cluster, reproduce a thesis scenario (P1–P10, S1–S3, F, G) or check agreement with real drains.
argument-hint: "[scenario id, e.g. p1 or s2] [create|run]"
---

# Scenario test on kind

Scenario: $0 · action: $1 (default: run)

Every scenario lives in `test/scenarios/<id>/` and follows `test/scenarios/basic/`:

| File | Content |
|---|---|
| `kind.yaml` | cluster layout; node image `kindest/node:v1.36.4` (see CLAUDE.md, Environment) |
| `*.yaml` | the workloads, PDBs and namespaces that make exactly this scenario occur |
| `run.sh` | the whole run, see below |
| `README.md` | what the scenario shows, which FR/NFR/scenario it covers, expected result, how to run |

## run.sh contract

- `#!/usr/bin/env bash` and `set -euo pipefail`; idempotent.
- `export KUBECONFIG="$OUT/kind.kubeconfig"` before `kind create cluster`, so
  `~/.kube/config` is never changed; delete the cluster on exit with `trap`
  unless `KEEP=1`.
- Use `kubectl --context kind-<cluster>` explicitly for every command.
- Wait until workloads are Ready and PDBs report the expected allowed
  disruptions before taking the snapshot.
- Take the snapshot with the token of `drainsim-collector` from
  `test/rbac/readonly.yaml` in a separate kubeconfig (NFR1), and check with
  `kubectl auth can-i` that it can neither evict nor patch nodes.
- Run drainsim (`$DRAINSIM`, built for linux) in text and JSON and keep the
  exit codes.
- Run the real `kubectl drain --ignore-daemonsets` on the same nodes, with a
  timeout where it is expected to block, and record where pods end up.
- Print one comparison line per node: simulated vs real (drained or not, and
  where the pods went). Exit non-zero if they disagree.

## When creating a scenario

1. Load the thesis-design skill and state which scenario and requirements the
   cluster reproduces.
2. Build the smallest cluster in which exactly that scenario occurs.
3. Run it. If simulation and reality differ, find the cause. Fix drainsim in a
   separate commit with a unit test that reproduces the difference; record any
   deliberate difference with the record-decision skill.

## Report

Short table: node · simulated · real · agree. Then bugs found, commits made,
and whether the cluster was deleted.
