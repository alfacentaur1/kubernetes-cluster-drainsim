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

rm -rf "$OUT" && mkdir -p "$OUT"

if [[ -z "${DRAINSIM:-}" ]]; then
  log "building drainsim"
  (cd "$ROOT" && go build -o "$OUT/drainsim" ./cmd/drainsim)
  DRAINSIM="$OUT/drainsim"
fi

# --- 1. cluster and workload -------------------------------------------------
log "creating cluster $CLUSTER"
kind delete cluster --name "$CLUSTER" >/dev/null 2>&1 || true
kind create cluster --name "$CLUSTER" --config "$HERE/kind.yaml" --wait 120s

k apply -f "$HERE/web.yaml"
k rollout status deployment/web --timeout=180s
k wait pod -l app=web --for=condition=Ready --timeout=120s
for _ in $(seq 60); do
  [[ "$(k get pdb web -o jsonpath='{.status.disruptionsAllowed}')" == 1 ]] && break
  sleep 1
done
[[ "$(k get pdb web -o jsonpath='{.status.disruptionsAllowed}')" == 1 ]] ||
  { echo "PDB web does not allow 1 disruption" >&2; exit 1; }
k get pods -A -o wide >"$OUT/pods-before.txt"

# --- 2. read-only snapshot (NFR1) --------------------------------------------
log "taking a snapshot with the read-only service account"
k apply -f "$ROOT/test/rbac/readonly.yaml"
RO="$OUT/readonly.kubeconfig"
SERVER="$(k config view --raw --minify -o jsonpath='{.clusters[0].cluster.server}')"
k config view --raw --minify -o jsonpath='{.clusters[0].cluster.certificate-authority-data}' |
  base64 -d >"$OUT/ca.crt"
TOKEN="$(k -n default create token drainsim-collector --duration=1h)"
# A kubeconfig with only the token: no admin credentials can leak in.
kubectl --kubeconfig "$RO" config set-cluster "$CLUSTER" --server "$SERVER" \
  --certificate-authority "$OUT/ca.crt" --embed-certs >/dev/null
kubectl --kubeconfig "$RO" config set-credentials drainsim-collector --token "$TOKEN" >/dev/null
kubectl --kubeconfig "$RO" config set-context readonly --cluster "$CLUSTER" \
  --user drainsim-collector >/dev/null
# The service account must not be able to evict or cordon.
echo "can evict: $(kubectl --kubeconfig "$RO" --context readonly auth can-i create pods/eviction -n default || true)"
echo "can patch nodes: $(kubectl --kubeconfig "$RO" --context readonly auth can-i patch nodes || true)"

snap_rc=0
"$DRAINSIM" snapshot --kubeconfig "$RO" --context readonly -o "$OUT/snapshot.json" \
  2>"$OUT/snapshot.err" || snap_rc=$?
echo "snapshot exit code: $snap_rc"
[[ $snap_rc -eq 0 ]] || { cat "$OUT/snapshot.err" >&2; exit 1; }

# --- 3. simulated drains -----------------------------------------------------
declare -A SIM
simulate() { # simulate NAME NODES
  local name=$1 nodes=$2 rc
  for fmt in text json; do
    rc=0
    "$DRAINSIM" drain "$OUT/snapshot.json" --nodes "$nodes" --format "$fmt" \
      >"$OUT/sim-$name.$fmt" 2>"$OUT/sim-$name.$fmt.err" || rc=$?
    echo "drainsim drain --nodes $nodes --format $fmt: exit code $rc"
    SIM[$name.$fmt]=$rc
  done
}
log "simulating"
simulate w1 "$W1"
simulate w1w2 "$W1,$W2"

# --- 4. real drains ----------------------------------------------------------
declare -A REAL
placement() { # placement FILE: where the pods of web are, and pending ones
  k get pods -l app=web -o custom-columns=POD:.metadata.name,PHASE:.status.phase,NODE:.spec.nodeName --no-headers \
    | sort | tee "$1"
}
real_drain() { # real_drain NAME NODE TIMEOUT
  local name=$1 node=$2 timeout=$3 rc=0
  k drain "$node" --ignore-daemonsets --timeout="$timeout" >"$OUT/real-$name.log" 2>&1 || rc=$?
  echo "kubectl drain $node: exit code $rc"
  REAL[$name]=$rc
  placement "$OUT/real-$name.pods"
}
log "draining $W1 for real"
real_drain w1 "$W1" 180s
# Give the last replacement time to become Ready before the next drain.
k rollout status deployment/web --timeout=60s || true
log "draining $W2 for real (after $W1)"
real_drain w1w2 "$W2" 60s

