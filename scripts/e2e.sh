#!/usr/bin/env bash
# Full pipeline: cluster up -> terraform -> CRD -> controller ->
# gitopsd apply + drift correction. Artifacts land in results/.
set -uo pipefail
cd "$(dirname "$0")/.."

export PATH=/home/asdf/.local/opt/k8s-lab/bin:$PATH
K="kubectl --kubeconfig $PWD/cluster/kubeconfig"
R=results
mkdir -p "$R"
LOG=$R/e2e.log
: > "$LOG"
say() { echo "+ $*" | tee -a "$LOG"; }

bash scripts/up.sh | tee -a "$LOG"

say "apply CRD"
$K apply -f manifests/crd/previewapp.yaml | tee -a "$LOG"
$K get crd previewapps.platform.lab | tee -a "$LOG"

say "terraform init+apply"
( cd terraform && terraform init -no-color > "../$R/tf_init.log" 2>&1 ) \
    | tee -a "$LOG"
( cd terraform && terraform apply -auto-approve -no-color \
    > "../$R/tf_apply.log" 2>&1 )
tail -4 "$R/tf_apply.log" | tee -a "$LOG"

say "build + start previewctl"
go build -o cluster/bin/previewctl ./cmd/previewctl 2>&1 | tee -a "$LOG"
pgrep -f 'cluster/bin/previewctl' >/dev/null || {
    setsid ./cluster/bin/previewctl -kubeconfig cluster/kubeconfig \
        -namespace preview >> "$R/previewctl.log" 2>&1 < /dev/null &
    sleep 1
}

say "apply gitops-managed PreviewApp"
$K apply -f manifests/apps/preview-cr.yaml | tee -a "$LOG"

for i in $(seq 40); do
    $K -n preview get deploy pva-tf-demo pva-gitops-demo >/dev/null 2>&1 \
        && break
    sleep 0.5
done

say "controller state"
{
    $K -n preview get previewapps
    $K -n preview get previewapp tf-demo -o jsonpath='{.status}'
    echo
    $K -n preview get deploy,svc,pods -o wide
} | tee -a "$LOG" > "$R/controller_state.txt"

say "gitopsd initial apply"
go build -o cluster/bin/gitopsd ./cmd/gitopsd 2>&1 | tee -a "$LOG"
./cluster/bin/gitopsd -kubeconfig cluster/kubeconfig \
    -dir gitops-repo/deploy -once | tee -a "$LOG" > "$R/gitops_apply1.txt"

say "edit manifest: replicas 2 -> 3"
sed -i 's/replicas: 2/replicas: 3/' gitops-repo/deploy/apps.yaml
./cluster/bin/gitopsd -kubeconfig cluster/kubeconfig \
    -dir gitops-repo/deploy -once | tee -a "$LOG" > "$R/gitops_apply2.txt"

say "out-of-band drift: scale to 5, then converge"
$K -n preview scale deploy store-front --replicas=5 | tee -a "$LOG"
$K -n preview get deploy store-front \
    -o jsonpath='{.spec.replicas} replicas before converge' | tee -a "$LOG"
echo | tee -a "$LOG"
./cluster/bin/gitopsd -kubeconfig cluster/kubeconfig \
    -dir gitops-repo/deploy -once | tee -a "$LOG" > "$R/gitops_apply3.txt"
$K -n preview get deploy store-front \
    -o jsonpath='{.spec.replicas} replicas after converge' | tee -a "$LOG"
echo | tee -a "$LOG"

say "final state"
{
    $K -n preview get all -o wide
    $K -n preview get previewapps
} | tee "$R/final_state.txt" -a "$LOG"

say "unit tests"
go test ./... | tee "$R/go_test.txt" -a "$LOG"
