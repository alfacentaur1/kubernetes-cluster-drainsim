#!/usr/bin/env bash
# Scenario F1 (right-sizing, FR20–FR23, NFR1, NFR20): drainsim reads the
# usage of the workloads from Prometheus, right-sizes their requests and
# finds the nodes that can then be removed. The script applies the same
# requests to the real cluster and drains those nodes. See README.md.
#
# Environment:
#   DRAINSIM  path of a Linux drainsim binary (default: built with go)
#   CLUSTER   name of the kind cluster (default: drainsim-f1)
#   OUT       directory for snapshot, reports and logs (default: ./out)
#   SETTLE    seconds the workloads run before the snapshot (default: 960)
#   KEEP=1    keep the cluster at the end (default: delete it)
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$HERE/../../.." && pwd)"
CLUSTER="${CLUSTER:-drainsim-f1}"
OUT="${OUT:-$HERE/out}"
SETTLE="${SETTLE:-960}"
CTX="kind-$CLUSTER"
WORKERS=("$CLUSTER-worker" "$CLUSTER-worker2" "$CLUSTER-worker3")
CHART_VERSION=92.13.1 # kube-prometheus-stack, see README.md
WINDOW=15m
PROM_PORT=19090

log() { printf '\n==> %s\n' "$*"; }
k() { kubectl --context "$CTX" "$@"; }

for tool in docker kind kubectl jq helm curl; do
  command -v "$tool" >/dev/null || { echo "missing tool: $tool" >&2; exit 2; }
done

rm -rf "$OUT" && mkdir -p "$OUT"

if [[ -z "${DRAINSIM:-}" ]]; then
  log "building drainsim"
  (cd "$ROOT" && go build -o "$OUT/drainsim" ./cmd/drainsim)
  DRAINSIM="$OUT/drainsim"
fi

# kind, kubectl and helm use a kubeconfig of their own in $OUT, so the
# scenario never changes ~/.kube/config.
export KUBECONFIG="$OUT/kind.kubeconfig"
PF_PID=""
cleanup() {
  local rc=$? # keep the exit code of the script
  [[ -n "$PF_PID" ]] && kill "$PF_PID" 2>/dev/null || true
  if [[ "${KEEP:-}" != 1 ]]; then
    log "deleting cluster $CLUSTER (KEEP=1 keeps it)"
    kind delete cluster --name "$CLUSTER" >/dev/null 2>&1 || true
  fi
  exit "$rc"
}
trap cleanup EXIT

# --- 1. cluster, monitoring, workloads ---------------------------------------
log "creating cluster $CLUSTER"
kind delete cluster --name "$CLUSTER" >/dev/null 2>&1 || true
kind create cluster --name "$CLUSTER" --config "$HERE/kind.yaml" --wait 120s
for w in "${WORKERS[@]}"; do
  echo "$w allocatable: $(k get node "$w" -o jsonpath='{.status.allocatable.cpu} CPU, {.status.allocatable.memory}')"
done

log "installing kube-prometheus-stack $CHART_VERSION"
helm repo add prometheus-community https://prometheus-community.github.io/helm-charts --force-update >/dev/null
helm --kube-context "$CTX" install kps prometheus-community/kube-prometheus-stack \
  --version "$CHART_VERSION" --namespace monitoring --create-namespace \
  --values "$HERE/prometheus-values.yaml" --wait --timeout 10m >"$OUT/helm.log"
k -n monitoring rollout status statefulset/prometheus-kps-prometheus --timeout=300s

log "deploying the workloads"
k apply -f "$HERE/workloads.yaml"
for d in over-a over-b hungry; do k -n shop rollout status "deployment/$d" --timeout=300s; done
k get pods -A -o wide >"$OUT/pods-before.txt"
k -n shop get pods -o wide

# --- 2. Prometheus ------------------------------------------------------------
# The port-forward uses the admin kubeconfig of the scenario; drainsim only
# sends GET /api/v1/query through it.
k -n monitoring port-forward svc/kps-prometheus "$PROM_PORT:9090" >"$OUT/port-forward.log" 2>&1 &
PF_PID=$!
PROM="http://127.0.0.1:$PROM_PORT"
for _ in $(seq 60); do curl -fs "$PROM/-/ready" >/dev/null && break; sleep 1; done
curl -fs "$PROM/-/ready" >/dev/null || { echo "Prometheus is not ready" >&2; exit 1; }

log "letting the workloads run for $SETTLE s (usage window $WINDOW)"
sleep "$SETTLE"

promq() { curl -fsG "$PROM/api/v1/query" --data-urlencode "query=$1"; }
promq 'kube_pod_owner{namespace="shop"}' >"$OUT/raw-kube_pod_owner.json"
promq 'container_cpu_usage_seconds_total{namespace="shop"}' >"$OUT/raw-container_cpu.json"
echo "kube_pod_owner rows in shop: $(jq '.data.result | length' "$OUT/raw-kube_pod_owner.json")"
echo "container_cpu_usage_seconds_total series in shop: $(jq '.data.result | length' "$OUT/raw-container_cpu.json")"

# Usage observed independently of the collector, per pod: the mean CPU over
# the last 10 min and the maximum working set over the window.
promq 'avg_over_time(rate(container_cpu_usage_seconds_total{namespace="shop",container!=""}[1m])[10m:15s])' >"$OUT/raw-observed-cpu.json"
promq "max_over_time(container_memory_working_set_bytes{namespace=\"shop\",container!=\"\"}[$WINDOW])" >"$OUT/raw-observed-mem.json"
jq -n --slurpfile c "$OUT/raw-observed-cpu.json" --slurpfile m "$OUT/raw-observed-mem.json" '
  [$c[0].data.result[] | {pod: .metric.pod, container: .metric.container, cpu: (.value[1] | tonumber)}] as $cpu
  | [$m[0].data.result[] | {pod: .metric.pod, container: .metric.container, mem: (.value[1] | tonumber)}] as $mem
  | $cpu | map(. as $x | . + {mem: ([$mem[] | select(.pod == $x.pod and .container == $x.container)][0].mem // 0)})'   >"$OUT/observed.json"

# NFR1: the admin API (the only part of Prometheus that writes) is disabled,
# so the snapshot works with read access alone.
admin=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$PROM/api/v1/admin/tsdb/snapshot")
echo "Prometheus admin API (POST /api/v1/admin/tsdb/snapshot): HTTP $admin"
[[ "$admin" != 200 ]] || { echo "the admin API is enabled" >&2; exit 1; }

# --- 3. read-only snapshot with usage (NFR1, FR20) ---------------------------
log "taking a snapshot with the read-only service account and --prometheus"
k apply -f "$ROOT/test/rbac/readonly.yaml"
RO="$OUT/readonly.kubeconfig"
SERVER="$(k config view --raw --minify -o jsonpath='{.clusters[0].cluster.server}')"
k config view --raw --minify -o jsonpath='{.clusters[0].cluster.certificate-authority-data}' |
  base64 -d >"$OUT/ca.crt"
TOKEN="$(k -n default create token drainsim-collector --duration=1h)"
kubectl --kubeconfig "$RO" config set-cluster "$CLUSTER" --server "$SERVER" \
  --certificate-authority "$OUT/ca.crt" --embed-certs >/dev/null
kubectl --kubeconfig "$RO" config set-credentials drainsim-collector --token "$TOKEN" >/dev/null
kubectl --kubeconfig "$RO" config set-context readonly --cluster "$CLUSTER" \
  --user drainsim-collector >/dev/null
echo "can evict: $(kubectl --kubeconfig "$RO" --context readonly auth can-i create pods/eviction -n shop || true)"
echo "can patch nodes: $(kubectl --kubeconfig "$RO" --context readonly auth can-i patch nodes || true)"
echo "can port-forward: $(kubectl --kubeconfig "$RO" --context readonly auth can-i create pods/portforward -n monitoring || true)"

"$DRAINSIM" snapshot --kubeconfig "$RO" --context readonly -o "$OUT/snapshot.json" \
  --prometheus "$PROM" --usage-window "$WINDOW" 2>"$OUT/snapshot.err" ||
  { cat "$OUT/snapshot.err" >&2; exit 1; }
jq -r '.usageProfiles[] | "profile \(.namespace)/\(.kind)/\(.name): " +
  ([.containers[] | "\(.name) cpu p95 \(.cpuP95Millicores)m, mem max \(.memoryMaxBytes / 1048576 | floor)Mi"] | join("; "))' \
  "$OUT/snapshot.json" | grep ' shop/' || { echo "no usage profiles for namespace shop" >&2; exit 1; }

# --- 4. simulation ------------------------------------------------------------
log "simulating: consolidate --right-size"
NODES="$(IFS=,; echo "${WORKERS[*]}")"
for fmt in text json; do
  rc=0
  "$DRAINSIM" consolidate "$OUT/snapshot.json" --right-size --config "$ROOT/examples/drainsim.yaml" \
    --nodes "$NODES" --format "$fmt" >"$OUT/sim.$fmt" 2>"$OUT/sim.$fmt.err" || rc=$?
  echo "drainsim consolidate --right-size --format $fmt: exit code $rc"
  [[ $rc -eq 0 ]] || { cat "$OUT/sim.$fmt.err" >&2; exit 1; }
done
cat "$OUT/sim.text"
mapfile -t SIM_CURRENT < <(jq -r '(.consolidation.rightSizing.current.removed // [])[]' "$OUT/sim.json")
mapfile -t SIM_REMOVED < <(jq -r '(.consolidation.removed // [])[]' "$OUT/sim.json")
[[ ${#SIM_REMOVED[@]} -gt 0 ]] || echo "WARNING: the simulation finds no removable node after right-sizing"

# --- 5. real counterpart --------------------------------------------------------
all_running() { # all_running: every pod of shop is Running and Ready, none Pending
  local bad
  bad=$(k -n shop get pods -o json | jq -r '.items[] | select(.metadata.deletionTimestamp == null)
    | select(.status.phase != "Running" or ([.status.conditions[]? | select(.type == "Ready" and .status == "True")] | length) == 0)
    | .metadata.name')
  [[ -z "$bad" ]] || { echo "not running: $bad"; return 1; }
}
wait_running() { # wait_running SECONDS
  for _ in $(seq "$1"); do all_running >/dev/null && return 0; sleep 1; done
  all_running
}

# With the current requests: draining the first candidate leaves pods
# Pending, so no node can be removed (the simulation says so too).
FIRST="$(jq -r '.order[0]' "$OUT/sim.json")"
log "real drain of $FIRST with the current requests"
rc=0
k drain "$FIRST" --ignore-daemonsets --timeout=120s >"$OUT/real-current.log" 2>&1 || rc=$?
echo "kubectl drain $FIRST: exit code $rc"
sleep 20
REAL_CURRENT=removable
all_running >"$OUT/real-current.pending" || REAL_CURRENT=kept
cat "$OUT/real-current.pending"
k -n shop get pods -o wide >"$OUT/real-current.pods"
k uncordon "$FIRST"
wait_running 300

# Right-sized: every recommendation that kubectl can apply, as the
# simulation applies them all (kindnet, for example, also holds requests on
# every worker). Static pods (kind Node, only on the control plane) cannot be
# changed this way, and the monitoring stack is managed by its operator,
# which would undo the change; both run only on the control plane, which is
# not a candidate. Lower requests first, so that the raised ones find room
# during their rollout.
log "applying the recommended requests with kubectl set resources"
jq -r '.consolidation.rightSizing.recommendations
  | sort_by(.higher) | .[] | select((.kind == "Deployment" or .kind == "StatefulSet" or .kind == "DaemonSet")
    and .namespace != "monitoring" and (.noData | not))
  | "\(.namespace) \(.kind | ascii_downcase) \(.name) \(.container) \(.recommendedCpuMillicores)m \(.recommendedMemoryBytes)"' \
  "$OUT/sim.json" | tee "$OUT/applied.txt" |
  while read -r ns kind name container cpu mem; do
    k -n "$ns" set resources "$kind/$name" -c "$container" --requests="cpu=$cpu,memory=$mem"
    k -n "$ns" rollout status "$kind/$name" --timeout=300s
  done
wait_running 300
k -n shop get pods -o wide | tee "$OUT/real-rightsized-before.pods"

declare -A REAL
for n in "${SIM_REMOVED[@]}"; do
  log "real drain of $n with the right-sized requests"
  rc=0
  k drain "$n" --ignore-daemonsets --timeout=180s >"$OUT/real-$n.log" 2>&1 || rc=$?
  echo "kubectl drain $n: exit code $rc"
  sleep 20
  if [[ $rc -eq 0 ]] && wait_running 120 >"$OUT/real-$n.pending"; then REAL[$n]=removed; else REAL[$n]="pods not running"; fi
  cat "$OUT/real-$n.pending"
done
k -n shop get pods -o wide | tee "$OUT/real-rightsized-after.pods"

# --- 6. comparison ------------------------------------------------------------------
log "recommended vs observed usage per container"
# observed = the busiest pod of the workload (mean CPU over 10 min, max memory)
printf '%-18s %-6s | %9s %9s %11s | %9s %9s %11s\n' workload cont "cpu p95" observed recommended "mem max" observed recommended
jq -r '.consolidation.rightSizing.recommendations[] | select(.namespace == "shop")
  | [.name, .container, .recommendedCpuMillicores, .recommendedMemoryBytes, .higher] | @tsv' "$OUT/sim.json" |
  while IFS=$'\t' read -r name container cpu mem higher; do
    read -r p95 mmax < <(jq -r --arg n "$name" --arg c "$container" '.usageProfiles[]
      | select(.namespace == "shop" and .name == $n) | .containers[] | select(.name == $c)
      | "\(.cpuP95Millicores) \(.memoryMaxBytes / 1048576 | floor)"' "$OUT/snapshot.json")
    read -r ocpu omem < <(jq -r --arg n "$name" --arg c "$container" '[.[] | select((.pod | startswith($n + "-")) and .container == $c)]
      | "\(map(.cpu) | max * 1000 | floor) \(map(.mem) | max / 1048576 | floor)"' "$OUT/observed.json")
    printf '%-18s %-6s | %8sm %8sm %10sm | %7sMi %7sMi %9sMi %s\n' "Deployment/$name" "$container" \
      "$p95" "$ocpu" "$cpu" "$mmax" "$omem" "$((mem / 1048576))" "$([[ $higher == true ]] && echo '(higher)' || true)"
  done

log "simulated vs real"
status=0
printf '%-40s %-12s %-18s %s\n' case simulated real agree
cur_sim=$([[ " ${SIM_CURRENT[*]} " == *" $FIRST "* ]] && echo removable || echo kept)
agree=$([[ $cur_sim == "$REAL_CURRENT" ]] && echo yes || { status=1; echo NO; })
printf '%-40s %-12s %-18s %s\n' "current requests / $FIRST" "$cur_sim" "$REAL_CURRENT" "$agree"
for n in "${WORKERS[@]}"; do
  if [[ " ${SIM_REMOVED[*]} " == *" $n "* ]]; then
    agree=$([[ ${REAL[$n]} == removed ]] && echo yes || { status=1; echo NO; })
    printf '%-40s %-12s %-18s %s\n' "right-sized / $n" removed "${REAL[$n]}" "$agree"
  else
    printf '%-40s %-12s %-18s %s\n' "right-sized / $n" kept "(not drained)" -
  fi
done
echo
# The scenario is built so that right-sizing frees one worker (README.md).
[[ ${#SIM_REMOVED[@]} -eq 1 ]] || { echo "expected 1 removable worker after right-sizing, got ${#SIM_REMOVED[@]}"; status=1; }
echo "simulated removable with current requests: ${SIM_CURRENT[*]:-none}"
echo "simulated removable with right-sized requests: ${SIM_REMOVED[*]:-none}"
echo "Reports, logs and pod placement are in $OUT"
exit $status
