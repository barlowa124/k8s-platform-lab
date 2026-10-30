// previewctl: a PreviewApp controller. Watch CRs, reconcile a
// Deployment + Service per app, and write status back.

package main

import (
	"context"
	"flag"
	"log"
	"time"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/barlowa124/k8s-platform-lab/internal/controller"
)

func main() {
	var kubeconfig, ns string
	var resync time.Duration
	flag.StringVar(&kubeconfig, "kubeconfig",
		"cluster/kubeconfig", "path to kubeconfig")
	flag.StringVar(&ns, "namespace", "preview", "namespace to manage")
	flag.DurationVar(&resync, "resync", 10*time.Second, "resync period")
	flag.Parse()

	cfg, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		log.Fatal(err)
	}
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		log.Fatal(err)
	}
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		log.Fatal(err)
	}
	r := &controller.Reconciler{Dyn: dyn, Client: cs, NS: ns}
	log.Printf("previewctl watching previewapps in %q", ns)
	log.Fatal(r.Run(context.Background(), resync))
}
