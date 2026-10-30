#!/usr/bin/env bash
# Measure rolling-upgrade and pod-kill recovery on the kwok cluster.
# Writes results/recovery.log with timestamps and final states.
set -u
cd "$(dirname "$0")/.."
export PATH="$HOME/.local/opt/k8s-lab/bin:$PATH"
K="kubectl --kubeconfig cluster/kubeconfig"
LOG=results/recovery.log
mkdir -p results
: > "$LOG"

now_ms() { date -u +%s%3N; }
say() { echo "== $(date -u +%H:%M:%S.%3N) $*" | tee -a "$LOG"; }
wait_ready() { # ns deploy timeout_s
    local t=0
    while (( t < $3 )); do
        local r=$($K -n "$1" get deploy "$2" \
            -o jsonpath='{.status.readyReplicas}' 2>/dev/null)
        local w=$($K -n "$1" get deploy "$2" \
            -o jsonpath='{.spec.replicas}')
        [[ "${r:-0}" == "${w:-0}" ]] && return 0
        sleep 1; t=$((t+1))
    done
    return 1
}

say "baseline"
$K -n preview get pods -o wide 2>&1 | tee -a "$LOG"
$K -n preview get deploy 2>&1 | tee -a "$LOG"

# Stale pods carry Ready=False from an earlier node-NotReady window;
# deleting them lets kwok re-stage Ready=True on the replacements.
say "restage pods marked Ready=False by node lifecycle"
$K -n preview delete pods --all 2>&1 | tee -a "$LOG"
sleep 2
for d in store-front pva-tf-demo pva-gitops-demo; do
    wait_ready preview "$d" 60 || say "TIMEOUT waiting for $d"
done
$K -n preview get deploy 2>&1 | tee -a "$LOG"

say "rolling upgrade: store-front nginx:1.27 -> nginx:1.28"
old_rs=$($K -n preview get rs -l app=store-front \
    -o jsonpath='{.items[0].metadata.name}')
t0=$(now_ms)
$K -n preview set image deployment/store-front app=nginx:1.28 \
    2>&1 | tee -a "$LOG"
$K -n preview rollout status deployment/store-front --timeout=120s \
    2>&1 | tee -a "$LOG"
roll_ms=$(( $(now_ms) - t0 ))
say "rollout_ms=$roll_ms old_rs=$old_rs"
say "replicasets after rollout"
$K -n preview get rs -l app=store-front \
    -o custom-columns=NAME:.metadata.name,REPLICAS:.spec.replicas,READY:.status.readyReplicas \
    2>&1 | tee -a "$LOG"
say "per-pod create-to-ready latency after rollout"
for p in $($K -n preview get pods -l app=store-front -o name); do
    c=$($K -n preview get "$p" -o jsonpath='{.metadata.creationTimestamp}')
    r=$($K -n preview get "$p" \
        -o jsonpath='{.status.conditions[?(@.type=="Ready")].lastTransitionTime}')
    echo "  $p created=$c ready=$r" | tee -a "$LOG"
done

say "pod kill: pva-tf-demo"
pod=$($K -n preview get pods -l platform.lab/preview=tf-demo \
    -o jsonpath='{.items[0].metadata.name}')
t0=$(now_ms)
$K -n preview delete pod "$pod" 2>&1 | tee -a "$LOG"
kill_ms=-1
for ((i=0;i<900;i++)); do
    nready=$($K -n preview get pods -l platform.lab/preview=tf-demo \
        -o jsonpath='{range .items[*]}{.status.conditions[?(@.type=="Ready")].status}{"\n"}{end}' \
        | grep -c "True" || true)
    [[ "$nready" -ge 2 ]] && { kill_ms=$(( $(now_ms) - t0 )); break; }
    sleep 0.1
done
say "podkill_recovery_ms=$kill_ms killed=$pod"
say "pods after kill"
$K -n preview get pods -l platform.lab/preview=tf-demo 2>&1 | tee -a "$LOG"

sleep 11  # let one resync tick populate the gauges
say "metrics snapshot"
curl -s localhost:9090/metrics | grep -v "^#" | tee -a "$LOG"
say "done"
