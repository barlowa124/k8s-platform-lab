# Toolchain setup

Everything runs from `~/.local/opt/k8s-lab/bin` with no root and no
Docker. The versions below are what the committed artifacts were made
with.

```bash
mkdir -p ~/.local/opt/k8s-lab/{bin,dl}
cd ~/.local/opt/k8s-lab/dl

curl -sL -o go.tgz https://go.dev/dl/go1.27.3.linux-amd64.tar.gz
tar xzf go.tgz -C ..
ln -sf ../go/bin/go ../bin/go

curl -sL -o etcd.tgz \
  https://github.com/etcd-io/etcd/releases/download/v3.7.3/etcd-v3.7.3-linux-amd64.tar.gz
tar xzf etcd.tgz && cp etcd-v3.7.3-linux-amd64/etcd* ../bin/

for c in kube-apiserver kube-controller-manager kube-scheduler kubectl; do
  curl -sL -o $c https://dl.k8s.io/release/v1.37.2/bin/linux/amd64/$c
done
chmod +x kube-* kubectl && cp kube-apiserver kube-controller-manager \
  kube-scheduler kubectl ../bin/

curl -sL -o ../bin/kwok \
  https://github.com/kubernetes-sigs/kwok/releases/download/v0.8.0/kwok-linux-amd64
chmod +x ../bin/kwok

curl -sL -o tf.zip \
  https://releases.hashicorp.com/terraform/1.17.1/terraform_1.17.1_linux_amd64.zip
cd ../bin && unzip -o ../dl/tf.zip
```

Then `export PATH=$HOME/.local/opt/k8s-lab/bin:$PATH`.
