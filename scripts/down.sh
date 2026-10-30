#!/usr/bin/env bash
set -uo pipefail
# Kill by comm, not -f patterns: -f regexes can match the calling shell's
# own command line, and missed matches there left components running.
for name in previewctl gitopsd kube-apiserver kube-scheduler kwok \
    kube-controller etcd; do
    pkill -x "$name" 2>/dev/null || true
done
for i in $(seq 60); do
    pgrep -x kube-apiserver >/dev/null || break
    sleep 0.5
done
echo "down"
