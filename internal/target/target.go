// Package target resolves what the user typed (a pod, a workload, a label
// selector) into one concrete pod and container.
package target

import (
	"context"
	"fmt"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// DefaultContainerAnnotation is honoured by kubectl and so by us.
const DefaultContainerAnnotation = "kubectl.kubernetes.io/default-container"

// Chooser lets the caller pick one of several options (nil = not interactive).
type Chooser func(prompt string, options []string) (int, error)

var workloadKinds = map[string]string{
	"deploy": "deployment", "deployment": "deployment", "deployments": "deployment",
	"sts": "statefulset", "statefulset": "statefulset", "statefulsets": "statefulset",
	"ds": "daemonset", "daemonset": "daemonset", "daemonsets": "daemonset",
	"rs": "replicaset", "replicaset": "replicaset", "replicasets": "replicaset",
	"job": "job", "jobs": "job",
	"pod": "pod", "pods": "pod", "po": "pod",
}

// ParseRef splits "kind/name" into a canonical kind and name. A bare name
// means a pod.
func ParseRef(ref string) (kind, name string, err error) {
	k, n, ok := strings.Cut(ref, "/")
	if !ok {
		return "pod", ref, nil
	}
	canon, known := workloadKinds[strings.ToLower(k)]
	if !known {
		return "", "", fmt.Errorf("unsupported target kind %q (use pod, deploy, sts, ds, rs or job)", k)
	}
	if n == "" {
		return "", "", fmt.Errorf("missing name in %q", ref)
	}
	return canon, n, nil
}

// ResolvePod returns one running pod for ref, or for selector when ref is empty.
func ResolvePod(ctx context.Context, cs kubernetes.Interface, ns, ref, selector string, choose Chooser) (*corev1.Pod, error) {
	if ref == "" && selector == "" {
		return nil, fmt.Errorf("give a pod, workload (deploy/x) or -l selector")
	}
	if ref != "" {
		kind, name, err := ParseRef(ref)
		if err != nil {
			return nil, err
		}
		if kind == "pod" {
			pod, err := cs.CoreV1().Pods(ns).Get(ctx, name, metav1.GetOptions{})
			if err != nil {
				return nil, fmt.Errorf("getting pod %s/%s: %w", ns, name, err)
			}
			return pod, nil
		}
		sel, err := workloadSelector(ctx, cs, ns, kind, name)
		if err != nil {
			return nil, err
		}
		selector = sel
	}
	list, err := cs.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return nil, fmt.Errorf("listing pods: %w", err)
	}
	return pickPod(list.Items, ns, selector, choose)
}

// Selector returns the label selector of a workload.
func Selector(ctx context.Context, cs kubernetes.Interface, ns, kind, name string) (string, error) {
	return workloadSelector(ctx, cs, ns, kind, name)
}

func workloadSelector(ctx context.Context, cs kubernetes.Interface, ns, kind, name string) (string, error) {
	var ls *metav1.LabelSelector
	switch kind {
	case "deployment":
		o, err := cs.AppsV1().Deployments(ns).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return "", fmt.Errorf("getting deployment %s/%s: %w", ns, name, err)
		}
		ls = o.Spec.Selector
	case "statefulset":
		o, err := cs.AppsV1().StatefulSets(ns).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return "", fmt.Errorf("getting statefulset %s/%s: %w", ns, name, err)
		}
		ls = o.Spec.Selector
	case "daemonset":
		o, err := cs.AppsV1().DaemonSets(ns).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return "", fmt.Errorf("getting daemonset %s/%s: %w", ns, name, err)
		}
		ls = o.Spec.Selector
	case "replicaset":
		o, err := cs.AppsV1().ReplicaSets(ns).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return "", fmt.Errorf("getting replicaset %s/%s: %w", ns, name, err)
		}
		ls = o.Spec.Selector
	case "job":
		o, err := cs.BatchV1().Jobs(ns).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return "", fmt.Errorf("getting job %s/%s: %w", ns, name, err)
		}
		ls = o.Spec.Selector
	default:
		return "", fmt.Errorf("unsupported kind %q", kind)
	}
	if ls == nil {
		return "", fmt.Errorf("%s/%s has no selector", kind, name)
	}
	s, err := metav1.LabelSelectorAsSelector(ls)
	if err != nil {
		return "", fmt.Errorf("invalid selector on %s/%s: %w", kind, name, err)
	}
	return s.String(), nil
}

func ready(p *corev1.Pod) bool {
	for _, c := range p.Status.ContainerStatuses {
		if !c.Ready {
			return false
		}
	}
	return len(p.Status.ContainerStatuses) > 0
}

// pickPod prefers Running+Ready pods that are not terminating. When several
// remain it asks the chooser, or else takes the first by name.
func pickPod(pods []corev1.Pod, ns, selector string, choose Chooser) (*corev1.Pod, error) {
	var running, readyPods []corev1.Pod
	for _, p := range pods {
		if p.Status.Phase != corev1.PodRunning || p.DeletionTimestamp != nil {
			continue
		}
		running = append(running, p)
		if ready(&p) {
			readyPods = append(readyPods, p)
		}
	}
	cands := readyPods
	if len(cands) == 0 {
		cands = running
	}
	if len(cands) == 0 {
		return nil, fmt.Errorf("no running pod found in namespace %q for selector %q", ns, selector)
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].Name < cands[j].Name })
	if len(cands) == 1 || choose == nil {
		p := cands[0]
		return &p, nil
	}
	names := make([]string, len(cands))
	for i, p := range cands {
		names[i] = p.Name
	}
	idx, err := choose("pod", names)
	if err != nil {
		return nil, err
	}
	p := cands[idx]
	return &p, nil
}

// ResolveContainer picks the container: the explicit name, else the
// default-container annotation, else the only one, else asks the chooser.
func ResolveContainer(pod *corev1.Pod, explicit string, choose Chooser) (string, error) {
	names := make([]string, 0, len(pod.Spec.Containers))
	for _, c := range pod.Spec.Containers {
		names = append(names, c.Name)
	}
	if explicit != "" {
		for _, n := range names {
			if n == explicit {
				return n, nil
			}
		}
		return "", fmt.Errorf("container %q not found in pod %s (have: %s)", explicit, pod.Name, strings.Join(names, ", "))
	}
	if d := pod.Annotations[DefaultContainerAnnotation]; d != "" {
		for _, n := range names {
			if n == d {
				return n, nil
			}
		}
	}
	switch {
	case len(names) == 0:
		return "", fmt.Errorf("pod %s has no containers", pod.Name)
	case len(names) == 1:
		return names[0], nil
	case choose == nil:
		return "", fmt.Errorf("pod %s has several containers (%s); pass -c", pod.Name, strings.Join(names, ", "))
	}
	idx, err := choose("container", names)
	if err != nil {
		return "", err
	}
	return names[idx], nil
}
