package target

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func pod(name string, phase corev1.PodPhase, ready bool, labels map[string]string, containers ...string) *corev1.Pod {
	p := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: name, Labels: labels},
		Status:     corev1.PodStatus{Phase: phase, ContainerStatuses: []corev1.ContainerStatus{{Ready: ready}}},
	}
	if len(containers) == 0 {
		containers = []string{"app"}
	}
	for _, c := range containers {
		p.Spec.Containers = append(p.Spec.Containers, corev1.Container{Name: c})
	}
	return p
}

func TestParseRef(t *testing.T) {
	cases := []struct {
		in, kind, name string
		bad            bool
	}{
		{"web-1", "pod", "web-1", false},
		{"pod/web-1", "pod", "web-1", false},
		{"deploy/api", "deployment", "api", false},
		{"sts/db", "statefulset", "db", false},
		{"cronjob/x", "", "", true},
		{"deploy/", "", "", true},
	}
	for _, c := range cases {
		k, n, err := ParseRef(c.in)
		if (err != nil) != c.bad || k != c.kind || n != c.name {
			t.Errorf("ParseRef(%q) = %q,%q,%v", c.in, k, n, err)
		}
	}
}

func TestResolveDeploymentPicksReadyRunningPod(t *testing.T) {
	lbl := map[string]string{"app": "api"}
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "api"},
		Spec:       appsv1.DeploymentSpec{Selector: &metav1.LabelSelector{MatchLabels: lbl}},
	}
	cs := fake.NewSimpleClientset(dep,
		pod("api-crash", corev1.PodRunning, false, lbl),
		pod("api-good", corev1.PodRunning, true, lbl),
		pod("api-pending", corev1.PodPending, false, lbl),
	)
	got, err := ResolvePod(context.Background(), cs, "ns", "deploy/api", "", nil)
	if err != nil || got.Name != "api-good" {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestResolveNoRunningPod(t *testing.T) {
	lbl := map[string]string{"app": "x"}
	cs := fake.NewSimpleClientset(pod("x-1", corev1.PodPending, false, lbl))
	if _, err := ResolvePod(context.Background(), cs, "ns", "", "app=x", nil); err == nil {
		t.Error("expected error when nothing is running")
	}
	if _, err := ResolvePod(context.Background(), cs, "ns", "", "", nil); err == nil {
		t.Error("expected error without any target")
	}
}

func TestResolveChooserUsedForSeveral(t *testing.T) {
	lbl := map[string]string{"app": "x"}
	cs := fake.NewSimpleClientset(pod("x-a", corev1.PodRunning, true, lbl), pod("x-b", corev1.PodRunning, true, lbl))
	got, err := ResolvePod(context.Background(), cs, "ns", "", "app=x", func(_ string, opts []string) (int, error) { return 1, nil })
	if err != nil || got.Name != "x-b" {
		t.Errorf("got %v, %v", got, err)
	}
}

func TestResolveContainer(t *testing.T) {
	one := pod("p", corev1.PodRunning, true, nil, "main")
	if c, err := ResolveContainer(one, "", nil); err != nil || c != "main" {
		t.Errorf("single: %q %v", c, err)
	}
	two := pod("p", corev1.PodRunning, true, nil, "app", "sidecar")
	if _, err := ResolveContainer(two, "", nil); err == nil {
		t.Error("several containers without chooser should error")
	}
	if c, err := ResolveContainer(two, "sidecar", nil); err != nil || c != "sidecar" {
		t.Errorf("explicit: %q %v", c, err)
	}
	if _, err := ResolveContainer(two, "nope", nil); err == nil {
		t.Error("unknown container should error")
	}
	two.Annotations = map[string]string{DefaultContainerAnnotation: "sidecar"}
	if c, err := ResolveContainer(two, "", nil); err != nil || c != "sidecar" {
		t.Errorf("annotation: %q %v", c, err)
	}
}
