package controller

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	fakedyn "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"
)

func pva(name, image string, replicas, port int64) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "platform.lab/v1alpha1",
		"kind":       "PreviewApp",
		"metadata": map[string]any{
			"name": name, "namespace": "preview"},
		"spec": map[string]any{
			"image": image, "replicas": replicas, "port": port}}}
}

func newR(objs ...runtime.Object) *Reconciler {
	cs := fake.NewSimpleClientset()
	scheme := runtime.NewScheme()
	dyn := fakedyn.NewSimpleDynamicClient(scheme, objs...)
	return &Reconciler{Dyn: dyn, Client: cs, NS: "preview"}
}

func TestDesiredDeployment(t *testing.T) {
	d := DesiredDeployment(pva("demo", "img:v1", 3, 8080))
	if d.Name != "pva-demo" || *d.Spec.Replicas != 3 {
		t.Fatalf("bad deployment: %+v", d)
	}
	if d.Spec.Template.Spec.Containers[0].Ports[0].ContainerPort != 8080 {
		t.Fatal("port not propagated")
	}
}

func TestReconcileCreatesResources(t *testing.T) {
	r := newR(pva("demo", "img:v1", 2, 80))
	if err := r.Reconcile(context.Background(), "demo"); err != nil {
		t.Fatal(err)
	}
	d, err := r.Client.AppsV1().Deployments("preview").
		Get(context.Background(), "pva-demo", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("deployment missing: %v", err)
	}
	if *d.Spec.Replicas != 2 {
		t.Fatalf("replicas %d", *d.Spec.Replicas)
	}
	if _, err := r.Client.CoreV1().Services("preview").
		Get(context.Background(), "pva-demo", metav1.GetOptions{}); err != nil {
		t.Fatalf("service missing: %v", err)
	}
	after, err := r.pvas().Get(context.Background(), "demo",
		metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	st := after.Object["status"].(map[string]any)
	if st["deploymentName"] != "pva-demo" {
		t.Fatalf("status: %v", st)
	}
}

func TestReconcileIdempotent(t *testing.T) {
	r := newR(pva("demo", "img:v2", 1, 9090))
	ctx := context.Background()
	if err := r.Reconcile(ctx, "demo"); err != nil {
		t.Fatal(err)
	}
	if err := r.Reconcile(ctx, "demo"); err != nil {
		t.Fatalf("second reconcile: %v", err)
	}
	d, _ := r.Client.AppsV1().Deployments("preview").
		Get(ctx, "pva-demo", metav1.GetOptions{})
	if d.Spec.Template.Spec.Containers[0].Image != "img:v2" {
		t.Fatal("image drift after re-reconcile")
	}
}

func TestReconcileSpecChange(t *testing.T) {
	r := newR(pva("demo", "img:v1", 1, 80))
	ctx := context.Background()
	if err := r.Reconcile(ctx, "demo"); err != nil {
		t.Fatal(err)
	}
	p, _ := r.pvas().Get(ctx, "demo", metav1.GetOptions{})
	p.Object["spec"].(map[string]any)["replicas"] = int64(4)
	if _, err := r.pvas().Update(ctx, p, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := r.Reconcile(ctx, "demo"); err != nil {
		t.Fatal(err)
	}
	d, _ := r.Client.AppsV1().Deployments("preview").
		Get(ctx, "pva-demo", metav1.GetOptions{})
	if *d.Spec.Replicas != 4 {
		t.Fatalf("scale not applied: %d", *d.Spec.Replicas)
	}
}

func TestPhase(t *testing.T) {
	if phase(0, 2) != "Pending" || phase(1, 2) != "Progressing" ||
		phase(2, 2) != "Ready" || phase(0, 0) != "Scaled" {
		t.Fatal("phase mapping wrong")
	}
}
