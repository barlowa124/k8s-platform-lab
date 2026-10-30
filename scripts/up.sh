#!/usr/bin/env bash
# Bring up a control plane + kwok node entirely in user space.
# etcd, kube-apiserver, kube-controller-manager, kube-scheduler, kwok.
set -euo pipefail
cd "$(dirname "$0")/.."

BIN=${K8SLAB:-/home/asdf/.local/opt/k8s-lab/bin}
D=$PWD/cluster
mkdir -p "$D"/{pki,logs,etcd}

if [ ! -f "$D/pki/sa.key" ]; then
    openssl genrsa -out "$D/pki/sa.key" 2048 2>/dev/null
    openssl rsa -in "$D/pki/sa.key" -pubout -out "$D/pki/sa.pub" 2>/dev/null
    openssl req -x509 -new -key "$D/pki/sa.key" -subj /CN=lab-ca \
        -days 365 -out "$D/pki/ca.crt" 2>/dev/null
    echo 'admintoken,admin,admin,"system:masters"' > "$D/token.csv"
fi

PATH="$BIN:$PATH"

pgrep -f "etcd --data-dir $D" >/dev/null || {
    "$BIN/etcd" --data-dir "$D/etcd" \
        --listen-client-urls http://127.0.0.1:2379 \
        --advertise-client-urls http://127.0.0.1:2379 \
        > "$D/logs/etcd.log" 2>&1 &
}
for i in $(seq 30); do
    "$BIN/etcdctl" --endpoints http://127.0.0.1:2379 endpoint health 2>/dev/null | grep -q healthy && break
    sleep 0.3
done

pgrep -f "kube-apiserver --etcd-servers" >/dev/null || {
    "$BIN/kube-apiserver" \
        --etcd-servers=http://127.0.0.1:2379 \
        --bind-address=127.0.0.1 --secure-port=6443 \
        --service-cluster-ip-range=10.96.0.0/12 \
        --token-auth-file="$D/token.csv" \
        --authorization-mode=AlwaysAllow \
        --service-account-issuer=https://kubernetes.default.svc \
        --service-account-signing-key-file="$D/pki/sa.key" \
        --service-account-key-file="$D/pki/sa.pub" \
        --cert-dir="$D/pki" --allow-privileged=true \
        > "$D/logs/apiserver.log" 2>&1 &
}
for i in $(seq 60); do
    curl -sfk https://127.0.0.1:6443/readyz >/dev/null 2>&1 && break
    sleep 0.5
done

if [ ! -f "$D/kubeconfig" ]; then
    "$BIN/kubectl" --kubeconfig=/dev/null config set-cluster lab \
        --server=https://127.0.0.1:6443 --insecure-skip-tls-verify=true \
        --embed-certs=false >/dev/null 2>&1 || true
    "$BIN/kubectl" config set-cluster lab \
        --server=https://127.0.0.1:6443 --insecure-skip-tls-verify=true \
        --kubeconfig "$D/kubeconfig"
    "$BIN/kubectl" config set-credentials admin --token=admintoken \
        --kubeconfig "$D/kubeconfig"
    "$BIN/kubectl" config set-context lab --cluster=lab --user=admin \
        --kubeconfig "$D/kubeconfig"
    "$BIN/kubectl" config use-context lab --kubeconfig "$D/kubeconfig"
fi
K="$BIN/kubectl --kubeconfig $D/kubeconfig"
$K get --raw /readyz >/dev/null

pgrep -f "kube-controller-manager --kubeconfig" >/dev/null || {
    "$BIN/kube-controller-manager" \
        --kubeconfig="$D/kubeconfig" \
        --service-account-private-key-file="$D/pki/sa.key" \
        --root-ca-file="$D/pki/ca.crt" \
        --use-service-account-credentials \
        --leader-elect=false \
        > "$D/logs/kcm.log" 2>&1 &
}
pgrep -f "kube-scheduler --kubeconfig" >/dev/null || {
    "$BIN/kube-scheduler" --kubeconfig="$D/kubeconfig" \
        --leader-elect=false \
        > "$D/logs/sched.log" 2>&1 &
}
pgrep -f "kwok --kubeconfig" >/dev/null || {
    > "$D/kwok-stages.yaml"
    for f in node-initialize node-heartbeat pod-complete pod-delete \
             pod-ready; do
        cat "manifests/kwok/$f.yaml" >> "$D/kwok-stages.yaml"
        echo '---' >> "$D/kwok-stages.yaml"
    done
    setsid "$BIN/kwok" --kubeconfig="$D/kubeconfig" \
        -c "$D/kwok-stages.yaml" --manage-all-nodes=true \
        >> "$D/logs/kwok.log" 2>&1 < /dev/null &
}
sleep 1
$K apply -f - <<'EOF' >/dev/null
apiVersion: v1
kind: Node
metadata:
  name: kwok-node
EOF
echo "up: $($K get nodes --no-headers | wc -l) node(s)"
