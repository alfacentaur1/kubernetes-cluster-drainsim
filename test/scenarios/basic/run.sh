#!/usr/bin/env bash
# Basic scenario: compares the simulated drain of drainsim with the real
# kubectl drain on a kind cluster (NFR20, RQ1). See README.md.
#
# Environment:
#   DRAINSIM  path of a Linux drainsim binary (default: built with go)
#   CLUSTER   name of the kind cluster (default: drainsim-basic)
#   OUT       directory for snapshot, reports and logs (default: ./out)
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$HERE/../../.." && pwd)"
CLUSTER="${CLUSTER:-drainsim-basic}"
OUT="${OUT:-$HERE/out}"
CTX="kind-$CLUSTER"
W1="$CLUSTER-worker"
W2="$CLUSTER-worker2"

log() { printf '\n==> %s\n' "$*"; }
k() { kubectl --context "$CTX" "$@"; }

for tool in docker kind kubectl jq; do
  command -v "$tool" >/dev/null || { echo "missing tool: $tool" >&2; exit 2; }
done

