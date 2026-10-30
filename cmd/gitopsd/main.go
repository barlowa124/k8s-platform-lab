// gitopsd: a one-way GitOps reconcile loop. Poll a manifest directory,
// server-side apply every document, report per-object actions, and
// exit non-zero on any apply error. `--once` runs a single pass.

package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/barlowa124/k8s-platform-lab/internal/gitops"
)

func main() {
	var kubeconfig, dir string
	var interval time.Duration
	var once bool
	flag.StringVar(&kubeconfig, "kubeconfig",
		"cluster/kubeconfig", "path to kubeconfig")
	flag.StringVar(&dir, "dir", "gitops-repo/deploy", "manifest directory")
	flag.DurationVar(&interval, "interval", 5*time.Second, "poll period")
	flag.BoolVar(&once, "once", false, "apply once and exit")
	flag.Parse()

	cfg, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		log.Fatal(err)
	}
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		log.Fatal(err)
	}
	disc, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()
	prev := ""
	for {
		digest, err := gitops.DirDigest(dir)
		if err != nil {
			log.Fatal(err)
		}
		if digest != prev {
			res, err := gitops.ApplyDir(ctx, dyn, disc, dir)
			failed := report(res)
			if err != nil || failed {
				fmt.Fprintln(os.Stderr, "apply errors above")
				if once {
					os.Exit(1)
				}
			}
			prev = digest
			if once {
				return
			}
		}
		if once {
			return
		}
		time.Sleep(interval)
	}
}

func report(res []gitops.Result) bool {
	failed := false
	for _, r := range res {
		if r.Action == "error" {
			fmt.Printf("ERROR %s: %v\n", r.Key, r.Err)
			failed = true
		} else {
			fmt.Printf("%s %s\n", r.Action, r.Key)
		}
	}
	return failed
}
