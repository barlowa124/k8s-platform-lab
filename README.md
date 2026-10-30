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
  `status.phase`, `readyReplicas`, and the child names back.
- `cmd/gitopsd` is a one-way GitOps loop. It fingerprints a manifest
  directory, server-side applies every document with field manager
  `gitopsd`, and reports `created` / `configured` / `unchanged` per
  object.
- `terraform/` manages the `preview` namespace, a ConfigMap, and a
  `PreviewApp` instance through `kubernetes_manifest`.
- `manifests/kwok/` vendors the Stage CRD and the fast pod and node
  stages kwok needs to simulate pod lifecycle.
- `tests` live next to the code and run against client-go fakes.

## Run it

```bash
# toolchain is downloaded into user space by the notes in docs/setup.md
export PATH=$HOME/.local/opt/k8s-lab/bin:$PATH

bash scripts/up.sh        # control plane + kwok node
bash scripts/e2e.sh       # CRD + terraform + controller + gitops + drift
go test ./...
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
  binaries.
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
- The toolchain download is ~500 MB, and the cluster directory
  grows to ~200 MB of etcd data and logs.
- `up.sh` assumes amd64 Linux and ports 6443 and 2379 free.
