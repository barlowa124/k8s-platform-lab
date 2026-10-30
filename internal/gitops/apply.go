// Server-side apply helpers for the gitops loop. Each YAML document in
// the watched directory is decoded to unstructured and applied with
// FieldManager "gitopsd", then reported as created / updated / unchanged.

package gitops

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	k8syaml "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/restmapper"
)

const FieldManager = "gitopsd"

type Result struct {
	Key    string
	Action string // "created" | "configured" | "unchanged" | "error"
	Err    error
}

// ApplyDir applies every YAML doc in dir and returns per-object results.
func ApplyDir(ctx context.Context, dyn dynamic.Interface,
	disc discovery.DiscoveryInterface, dir string) ([]Result, error) {
	var files []string
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() &&
			(strings.HasSuffix(p, ".yaml") || strings.HasSuffix(p, ".yml")) {
			files = append(files, p)
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)

	gk, err := restmapper.GetAPIGroupResources(disc)
	if err != nil {
		return nil, err
	}
	rm := restmapper.NewDiscoveryRESTMapper(gk)

	var out []Result
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return out, err
		}
		dec := k8syaml.NewYAMLOrJSONDecoder(bufio.NewReader(
			strings.NewReader(string(data))), 4096)
		for {
			var obj unstructured.Unstructured
			err := dec.Decode(&obj)
			if err == io.EOF {
				break
			}
			if err != nil {
				out = append(out, Result{Key: f, Action: "error", Err: err})
				break
			}
			if obj.GetAPIVersion() == "" || obj.GetKind() == "" {
				continue
			}
			out = append(out, applyOne(ctx, dyn, rm, &obj))
		}
	}
	return out, nil
}

func applyOne(ctx context.Context, dyn dynamic.Interface,
	rm meta.RESTMapper, obj *unstructured.Unstructured) Result {
	gv, _ := schema.ParseGroupVersion(obj.GetAPIVersion())
	gk := gv.WithKind(obj.GetKind())
	m, err := rm.RESTMapping(gk.GroupKind(), gk.Version)
	key := fmt.Sprintf("%s/%s/%s", obj.GetAPIVersion(), obj.GetKind(),
		obj.GetName())
	if err != nil {
		return Result{Key: key, Action: "error", Err: err}
	}
	var ri dynamic.ResourceInterface
	if m.Scope.Name() == meta.RESTScopeNameNamespace {
		ns := obj.GetNamespace()
		if ns == "" {
			ns = "default"
			obj.SetNamespace(ns)
		}
		ri = dyn.Resource(m.Resource).Namespace(ns)
	} else {
		ri = dyn.Resource(m.Resource)
	}
	before, gerr := ri.Get(ctx, obj.GetName(), metav1.GetOptions{})
	b, err := obj.MarshalJSON()
	if err != nil {
		return Result{Key: key, Action: "error", Err: err}
	}
	_, err = ri.Patch(ctx, obj.GetName(), types.ApplyPatchType, b,
		metav1.PatchOptions{FieldManager: FieldManager, Force: ptr(true)})
	if err != nil {
		return Result{Key: key, Action: "error", Err: err}
	}
	switch {
	case apierrors.IsNotFound(gerr):
		return Result{Key: key, Action: "created"}
	case gerr == nil:
		after, _ := ri.Get(ctx, obj.GetName(), metav1.GetOptions{})
		if after != nil &&
			after.GetResourceVersion() != before.GetResourceVersion() {
			return Result{Key: key, Action: "configured"}
		}
		return Result{Key: key, Action: "unchanged"}
	default:
		return Result{Key: key, Action: "error", Err: gerr}
	}
}

// DirDigest is a cheap content fingerprint for change detection.
func DirDigest(dir string) (string, error) {
	var parts []string
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			b, _ := os.ReadFile(p)
			parts = append(parts, fmt.Sprintf("%s:%x", p, fnv(b)))
		}
		return err
	})
	sort.Strings(parts)
	return fmt.Sprintf("%x", fnv([]byte(strings.Join(parts, "|")))), err
}

func fnv(b []byte) uint64 {
	var h uint64 = 14695981039346656037
	for _, c := range b {
		h = (h ^ uint64(c)) * 1099511628211
	}
	return h
}

func ptr(v bool) *bool { return &v }
