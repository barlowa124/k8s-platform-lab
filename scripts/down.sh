#!/usr/bin/env bash
set -uo pipefail
cd "$(dirname "$0")/.."
D=$PWD/cluster
for pat in "etcd --data-dir $D" "kube-apiserver --etcd-servers" \
    "kube-controller-manager --kubeconfig" "kube-scheduler --kubeconfig" \
    "kwok --kubeconfig"; do
    pkill -f "$pat" 2>/dev/null || true
done
echo "down"
