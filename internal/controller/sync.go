// Sync logic for PreviewApp: CR spec -> Deployment + Service objects.
// Kept free of informer machinery so it is unit-testable with a fake
// clientset.

package controller

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	LabelManagedBy = "app.kubernetes.io/managed-by"
	ManagedBy      = "previewctl"
)

// DesiredDeployment renders the Deployment a PreviewApp asks for.
func DesiredDeployment(p *unstructured.Unstructured) *appsv1.Deployment {
	spec := p.Object["spec"].(map[string]any)
	name := fmt.Sprintf("pva-%s", p.GetName())
	replicas := int64(1)
	if r, ok := spec["replicas"].(int64); ok {
		replicas = r
	}
	labels := map[string]string{
		"platform.lab/preview": p.GetName(),
		LabelManagedBy:         ManagedBy,
	}
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: p.GetNamespace(),
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr32(int32(replicas)),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"platform.lab/preview": p.GetName()},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:  "app",
						Image: spec["image"].(string),
						Ports: []corev1.ContainerPort{{
							ContainerPort: int32(
								spec["port"].(int64))}},
					}},
				},
			},
		},
	}
}

// DesiredService renders the Service for the same PreviewApp.
func DesiredService(p *unstructured.Unstructured) *corev1.Service {
	spec := p.Object["spec"].(map[string]any)
	name := fmt.Sprintf("pva-%s", p.GetName())
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: p.GetNamespace(),
			Labels: map[string]string{
				"platform.lab/preview": p.GetName(),
				LabelManagedBy:         ManagedBy},
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				"platform.lab/preview": p.GetName()},
			Ports: []corev1.ServicePort{{
				Port:       int32(spec["port"].(int64)),
				TargetPort: intstr.FromInt32(int32(spec["port"].(int64))),
			}},
		},
	}
}

func ptr32(v int32) *int32 { return &v }
