// Reconcile loop: watch PreviewApp objects, converge Deployment and
// Service, write back status. Uses the dynamic client so no codegen is
// needed; correctness lives in sync.go.

package controller

import (
	"context"
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

var PreviewGVR = schema.GroupVersionResource{
	Group: "platform.lab", Version: "v1alpha1", Resource: "previewapps"}

type Reconciler struct {
	Dyn    dynamic.Interface
	Client kubernetes.Interface
	NS     string
}

func (r *Reconciler) pvas() dynamic.ResourceInterface {
	return r.Dyn.Resource(PreviewGVR).Namespace(r.NS)
}

// Reconcile brings the world to the CR's spec and updates status.
func (r *Reconciler) Reconcile(ctx context.Context, name string) error {
	p, err := r.pvas().Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil // deleted; owner ref GC handles children
	}
	if err != nil {
		return err
	}
	dep := DesiredDeployment(p)
	svc := DesiredService(p)

	if _, err := r.Client.AppsV1().Deployments(r.NS).Create(
		ctx, dep, metav1.CreateOptions{}); err != nil &&
		!apierrors.IsAlreadyExists(err) {
		return err
	} else if apierrors.IsAlreadyExists(err) {
		if _, err := r.Client.AppsV1().Deployments(r.NS).Update(
			ctx, dep, metav1.UpdateOptions{}); err != nil {
			return err
		}
	}
	if _, err := r.Client.CoreV1().Services(r.NS).Create(
		ctx, svc, metav1.CreateOptions{}); err != nil &&
		!apierrors.IsAlreadyExists(err) {
		return err
	}

	cur, _ := r.Client.AppsV1().Deployments(r.NS).Get(
		ctx, dep.Name, metav1.GetOptions{})
	var ready int64
	if cur != nil {
		ready = int64(cur.Status.ReadyReplicas)
	}
	status := map[string]any{
		"phase":          phase(ready, int64(*dep.Spec.Replicas)),
		"deploymentName": dep.Name,
		"serviceName":    svc.Name,
		"readyReplicas":  ready,
	}
	patch := map[string]any{"status": status}
	b, _ := toJSON(patch)
	_, err = r.pvas().Patch(ctx, name, types.MergePatchType, b,
		metav1.PatchOptions{}, "status")
	return err
}

func phase(ready, want int64) string {
	switch {
	case want == 0:
		return "Scaled"
	case ready >= want:
		return "Ready"
	case ready > 0:
		return "Progressing"
	default:
		return "Pending"
	}
}

// Run watches CRs and reconciles on every event, plus a periodic resync
// so status catches pod readiness changes it did not trigger.
func (r *Reconciler) Run(ctx context.Context, resync time.Duration) error {
	w, err := r.pvas().Watch(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	defer w.Stop()
	tick := time.NewTicker(resync)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ev, ok := <-w.ResultChan():
			if !ok {
				return fmt.Errorf("watch closed")
			}
			if ev.Type == watch.Added || ev.Type == watch.Modified {
				if u, ok := ev.Object.(*unstructured.Unstructured); ok {
					if err := r.Reconcile(ctx, u.GetName()); err != nil {
						fmt.Printf("reconcile %s: %v\n",
							u.GetName(), err)
					}
				}
			}
		case <-tick.C:
			list, err := r.pvas().List(ctx, metav1.ListOptions{})
			if err != nil {
				continue
			}
			for i := range list.Items {
				if err := r.Reconcile(ctx,
					list.Items[i].GetName()); err != nil {
					fmt.Printf("resync %s: %v\n",
						list.Items[i].GetName(), err)
				}
			}
		}
	}
}
