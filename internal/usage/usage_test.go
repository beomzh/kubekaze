package usage

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func rl(cpu, mem string) corev1.ResourceList {
	l := corev1.ResourceList{}
	if cpu != "" {
		l[corev1.ResourceCPU] = resource.MustParse(cpu)
	}
	if mem != "" {
		l[corev1.ResourceMemory] = resource.MustParse(mem)
	}
	return l
}

func TestNodesPercentAndOrder(t *testing.T) {
	raw := []byte(`{"items":[
	 {"metadata":{"name":"a"},"usage":{"cpu":"500m","memory":"1Gi"}},
	 {"metadata":{"name":"b"},"usage":{"cpu":"3600m","memory":"512Mi"}}]}`)
	nodes := []corev1.Node{
		{ObjectMeta: metav1.ObjectMeta{Name: "a"}, Status: corev1.NodeStatus{Allocatable: rl("2", "4Gi")}},
		{ObjectMeta: metav1.ObjectMeta{Name: "b"}, Status: corev1.NodeStatus{Allocatable: rl("4", "8Gi")}},
	}
	got, err := Nodes(raw, nodes)
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Name != "b" || got[0].CPUPercent != 90 || got[0].MemPercent != 6.25 {
		t.Errorf("b = %+v", got[0])
	}
	if got[1].CPUPercent != 25 || got[1].MemPercent != 25 {
		t.Errorf("a = %+v", got[1])
	}
}

func TestNodesUnknownAllocatableIsZeroPercent(t *testing.T) {
	got, err := Nodes([]byte(`{"items":[{"metadata":{"name":"x"},"usage":{"cpu":"1","memory":"1Mi"}}]}`), nil)
	if err != nil || got[0].CPUPercent != 0 || got[0].CPUMilli != 1000 {
		t.Errorf("%+v %v", got, err)
	}
}

func TestPodsLimitsAndUnits(t *testing.T) {
	raw := []byte(`{"items":[{"metadata":{"name":"p","namespace":"n"},"containers":[
	 {"name":"c1","usage":{"cpu":"100000000n","memory":"400Mi"}},
	 {"name":"c2","usage":{"cpu":"150m","memory":"102400Ki"}}]}]}`)
	pods := []corev1.Pod{{ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "n"}, Spec: corev1.PodSpec{Containers: []corev1.Container{
		{Name: "c1", Resources: corev1.ResourceRequirements{Requests: rl("100m", "256Mi"), Limits: rl("200m", "512Mi")}},
		{Name: "c2", Resources: corev1.ResourceRequirements{Limits: rl("300m", "512Mi")}},
	}}}}
	got, err := Pods(raw, pods)
	if err != nil {
		t.Fatal(err)
	}
	p := got[0]
	if p.CPUMilli != 250 || p.MemBytes != 500<<20 {
		t.Errorf("usage = %dm %d", p.CPUMilli, p.MemBytes)
	}
	if p.CPULimit != 500 || p.CPULimitPct != 50 || p.MemLimit != 1024<<20 {
		t.Errorf("limits = %+v", p)
	}
}

func TestPodsPartialLimitsAreNotReported(t *testing.T) {
	raw := []byte(`{"items":[{"metadata":{"name":"p","namespace":"n"},"containers":[{"name":"c1","usage":{"cpu":"100m","memory":"1Mi"}}]}]}`)
	pods := []corev1.Pod{{ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "n"}, Spec: corev1.PodSpec{Containers: []corev1.Container{
		{Name: "c1", Resources: corev1.ResourceRequirements{Limits: rl("200m", "")}},
		{Name: "side"},
	}}}}
	got, _ := Pods(raw, pods)
	if got[0].CPULimit != 0 || got[0].CPULimitPct != 0 {
		t.Errorf("limit should be unset: %+v", got[0])
	}
}

func TestBadQuantity(t *testing.T) {
	if _, err := Nodes([]byte(`{"items":[{"metadata":{"name":"x"},"usage":{"cpu":"abc"}}]}`), nil); err == nil {
		t.Error("want error")
	}
	if _, err := Pods([]byte(`not json`), nil); err == nil {
		t.Error("want error")
	}
}

func TestSortAndNamespaces(t *testing.T) {
	pods := []Pod{
		{Namespace: "a", Name: "1", CPUMilli: 100, MemBytes: 900},
		{Namespace: "b", Name: "2", CPUMilli: 300, MemBytes: 100},
		{Namespace: "a", Name: "3", CPUMilli: 250, MemBytes: 100},
	}
	SortPods(pods, SortCPU)
	if pods[0].Name != "2" {
		t.Errorf("cpu sort: %+v", pods)
	}
	SortPods(pods, SortMemory)
	if pods[0].Name != "1" {
		t.Errorf("mem sort: %+v", pods)
	}
	ns := ByNamespace(pods, SortCPU)
	if ns[0].Name != "a" || ns[0].CPUMilli != 350 || ns[0].Pods != 2 {
		t.Errorf("ns = %+v", ns)
	}
	if len(Limit(ns, 1)) != 1 || len(Limit(ns, 0)) != 2 {
		t.Error("limit")
	}
}

func TestFormat(t *testing.T) {
	cases := map[string]string{FormatCPU(250): "250m", FormatCPU(2000): "2", FormatCPU(1500): "1.50",
		FormatMem(512 << 20): "512Mi", FormatMem(3 << 29): "1.5Gi", FormatMem(2048): "2Ki"}
	for got, want := range cases {
		if got != want {
			t.Errorf("got %q want %q", got, want)
		}
	}
}
