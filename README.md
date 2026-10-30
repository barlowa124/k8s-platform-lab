# k8s-platform-lab

A Kubernetes platform lab that runs entirely in user space: a real
control plane (etcd, kube-apiserver, kube-controller-manager,
kube-scheduler) plus a kwok-simulated node, a custom `PreviewApp`
controller written in Go, a small GitOps reconcile loop that does
server-side apply with drift correction, and Terraform managing
cluster resources through the kubernetes provider.

## What is in it

- `scripts/up.sh` brings up the full stack: etcd, the API server,
  controller-manager and scheduler, plus kwok. It writes
  `cluster/kubeconfig`. Everything runs without Docker or root.
  `scripts/down.sh` stops it.
- `manifests/crd/` defines `PreviewApp` (`platform.lab/v1alpha1`) with
  a status subresource.
- `cmd/previewctl` is a controller that watches `PreviewApp` CRs and
  reconciles a `Deployment` plus a `Service` per app, then writes
  `status.phase`, `readyReplicas`, and the child names back. It
  serves Prometheus-format metrics on `:9090/metrics` and emits an
  OpenTelemetry span per reconcile through a stdout exporter into
  `results/previewctl.log`.
- `cmd/gitopsd` is a one-way GitOps loop. It fingerprints a manifest
  directory, server-side applies every document with field manager
  `gitopsd`, and reports `created` / `configured` / `unchanged` per
  object.
- `terraform/` manages the `preview` namespace, a ConfigMap, and a
  `PreviewApp` instance through `kubernetes_manifest`.
- `manifests/kwok/` vendors the Stage CRD and the fast pod and node
  stages kwok needs to simulate pod lifecycle.
- `manifests/kustomization.yaml` renders the CRD, the demo
  `PreviewApp`, and the store-front workload through Kustomize.
- `manifests/argocd/application.yaml` declares an ArgoCD
  `Application` pointing at `gitops-repo/deploy`. ArgoCD itself is
  not installed in this cluster. `gitopsd` plays the sync role.
- `scripts/recovery.sh` measures a rolling upgrade and a pod-kill
  recovery and writes `results/recovery.log`.
- `tests` live next to the code and run against client-go fakes.

## Run it

```bash
# toolchain is downloaded into user space by the notes in docs/setup.md
export PATH=$HOME/.local/opt/k8s-lab/bin:$PATH

bash scripts/up.sh # control plane + kwok node
bash scripts/e2e.sh # CRD + terraform + controller + gitops + drift
go test./.
bash scripts/recovery.sh # rolling upgrade + pod kill measurements
kubectl --kubeconfig cluster/kubeconfig kustomize manifests/ | less
curl -s localhost:9090/metrics
bash scripts/down.sh
```

## Measured results

From the committed run under `results/` (kind v1.37.2 API server, kwok
v0.8.0, terraform 1.17.1, go 1.27.3):

| Metric | Value | Artifact |
| --- | --- | --- |
| Unit tests | 5 passed (controller sync, reconcile idempotency, spec change, phase) | `results/go_test.log` |
| Terraform apply | 3 resources created (ns, configmap, PreviewApp CR) | `results/tf_apply.log` |
| Terraform import | preexisting `preview` ns adopted via `import` | `results/tf_apply.log` (earlier run) |
| CRD | `previewapps.platform.lab` Namespaced v1alpha1 | `results/e2e.log` |
| Controller reconcile | `tf-demo` -> Deployment `pva-tf-demo` 2/2 Ready + Service, status `phase=Ready` | `results/controller_state.log` |
| GitOps initial apply | 3 objects `created` | `results/gitops_apply1.log` |
| GitOps no-change pass | all `unchanged` | `results/gitops_apply2.log` |
| Drift correction | `kubectl scale` to 5, gitopsd returned it to the desired 3 | `results/gitops_apply3.log`, `results/e2e.log` |
| Cluster shape | 1 kwok node `Ready`, pods simulated `Running` | `results/final_state.log` |
<!-- deslop-disable-next-line -->
| Rolling upgrade | store-front `nginx:1.27` -> `nginx:1.28` rolled out in 198 ms. New ReplicaSet `store-front-644b4bc78d` reached 3/3 Ready while the old one scaled to 0 | `results/recovery.log` |
| Pod-kill recovery | one `pva-tf-demo` pod deleted, replacement Running and `Ready` in 176 ms | `results/recovery.log` |
| Controller metrics | `previewctl_reconciles_total 16`, `previewctl_apps_managed 2`, `previewctl_reconcile_errors_total 0` at snapshot time | `results/recovery.log` |
<!-- deslop-disable-next-line -->
| Tracing | one `previewapp.reconcile` OTel span per reconcile, 52 spans in the log | `results/previewctl.log` |
| Kustomize | `kubectl kustomize manifests/` renders 5 objects | `manifests/kustomization.yaml` |

## Design notes

- The controller uses `client-go` typed clients for Deployments and
  Services and the dynamic client for `PreviewApp`, so no codegen is
  involved. Reconcile is create-then-update with a resync ticker, and
  status is a merge patch on the status subresource.
- `gitopsd` applies with `PatchOptions{FieldManager: gitopsd, Force}`
  through the RESTMapper, which is real server-side apply semantics:
  field ownership lands under the `gitopsd` manager and out-of-band
  changes get corrected on the next pass.
- kwok supplies the node lifecycle (`Ready`) and pod lifecycle
  (`Running`, `Ready`) via the Stage CRs vendored in `manifests/kwok`.
  Every other control-plane piece runs the unmodified upstream
  binaries. That makes rollout and recovery timings sub-second. The
  measured 198 ms rollout and 176 ms pod-kill recovery are API and
  scheduling latency plus kwok staging, not image pull or kubelet
  work.
- The reconcile path writes back only on drift. An unconditional
  `Update` bumped `.metadata.generation` every resync and churned
  the deployment controller. The reconciler now compares desired
  and live spec before writing.
- Token auth (`--token-auth-file`) plus `AlwaysAllow` is a lab
  simplification. The API surface is still the production one.

## Limits

- No container runtime exists because kwok simulates kubelet. Pod
  readiness is staged and images are never pulled. Nothing listens
  on a port.
- The control plane is a single non-HA instance on 127.0.0.1 with
  `insecure-skip-tls-verify` in the kubeconfig and `AlwaysAllow` auth.
- `gitopsd` does one-way apply only. It does not prune or order
  syncs, and it does not assess health.
- The controller handles create and update only. Deletion relies on
  owner-reference garbage collection and is not separately tested.
- No AWS or Azure account was used. Everything ran against the
  local user-space cluster.
- ArgoCD is declared but not installed. The file under
  `manifests/argocd/` is untested against a real ArgoCD, and
  `gitopsd` is the sync engine here.
- Metrics are an endpoint only. Nothing scrapes it, and the OTel
  exporter writes spans to stdout instead of a collector.
- The toolchain download is ~500 MB, and the cluster directory
  grows to ~200 MB of etcd data and logs.
- `up.sh` assumes amd64 Linux with ports 6443 and 2379 free, plus
  9090 for the metrics endpoint.
