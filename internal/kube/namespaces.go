package kube

import (
	"context"
	"fmt"
	"sort"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

// OpenShiftProjectsGVR is the OpenShift project API.
var OpenShiftProjectsGVR = schema.GroupVersionResource{Group: "project.openshift.io", Version: "v1", Resource: "projects"}

// IsOpenShift reports whether the server exposes the OpenShift project API.
func IsOpenShift(cs kubernetes.Interface) bool {
	groups, err := cs.Discovery().ServerGroups()
	if err != nil || groups == nil {
		return false
	}
	for _, g := range groups.Groups {
		if g.Name == OpenShiftProjectsGVR.Group {
			return true
		}
	}
	return false
}

// ListNamespaces returns the namespaces (or OpenShift projects) the user may
// list, sorted, together with the noun for them ("namespace" or "project").
// On OpenShift, regular users usually cannot list namespaces but can list
// projects, so projects are preferred there.
func (o *Options) ListNamespaces(ctx context.Context) ([]string, string, error) {
	cfg, err := o.REST()
	if err != nil {
		return nil, "", err
	}
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, "", err
	}
	if IsOpenShift(cs) {
		dyn, err := dynamic.NewForConfig(cfg)
		if err != nil {
			return nil, "", err
		}
		list, err := dyn.Resource(OpenShiftProjectsGVR).List(ctx, metav1.ListOptions{})
		if err == nil {
			names := make([]string, 0, len(list.Items))
			for _, it := range list.Items {
				names = append(names, it.GetName())
			}
			sort.Strings(names)
			return names, "project", nil
		}
		// fall through to namespaces
	}
	nsList, err := cs.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, "", fmt.Errorf("listing namespaces: %w", err)
	}
	names := make([]string, 0, len(nsList.Items))
	for _, n := range nsList.Items {
		names = append(names, n.Name)
	}
	sort.Strings(names)
	return names, "namespace", nil
}
