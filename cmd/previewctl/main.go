// previewctl: a PreviewApp controller. Watch CRs, reconcile a
// Deployment + Service per app, and write status back.

package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"time"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/barlowa124/k8s-platform-lab/internal/controller"
	"github.com/barlowa124/k8s-platform-lab/internal/observe"
)

func main() {
	var kubeconfig, ns, metricsAddr string
	var resync time.Duration
	flag.StringVar(&kubeconfig, "kubeconfig",
		"cluster/kubeconfig", "path to kubeconfig")
	flag.StringVar(&ns, "namespace", "preview", "namespace to manage")
	flag.DurationVar(&resync, "resync", 10*time.Second, "resync period")
	flag.StringVar(&metricsAddr, "metrics-addr", ":9090",
		"address for the /metrics endpoint")
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
	m := observe.NewMetrics()
	tr, cleanup, err := observe.Tracer()
	if err != nil {
		log.Fatal(err)
	}
	defer cleanup()
	go func() {
		http.Handle("/metrics", m.Handler())
		log.Printf("metrics on %s/metrics", metricsAddr)
		_ = http.ListenAndServe(metricsAddr, nil)
	}()
	r := &controller.Reconciler{
		Dyn: dyn, Client: cs, NS: ns, Metrics: m, Tracer: tr}
	log.Printf("previewctl watching previewapps in %q", ns)
	log.Fatal(r.Run(context.Background(), resync))
}
