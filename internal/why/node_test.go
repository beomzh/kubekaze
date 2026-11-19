package why

import (
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/beomzh/kubekaze/internal/events"
)

func node(conds ...corev1.NodeCondition) *corev1.Node {
	return &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "n1"}, Status: corev1.NodeStatus{Conditions: conds}}
}

func healthyReady() corev1.NodeCondition {
	return corev1.NodeCondition{Type: corev1.NodeReady, Status: corev1.ConditionTrue, LastHeartbeatTime: metav1.NewTime(now.Add(-5 * time.Second))}
}

func TestNodeHealthy(t *testing.T) {
	if c := DiagnoseNode(NodeEvidence{Node: node(healthyReady()), Now: now}); c != nil {
		t.Errorf("got %+v", c)
	}
}

func TestNodeUnknownAndStale(t *testing.T) {
	unknown := corev1.NodeCondition{Type: corev1.NodeReady, Status: corev1.ConditionUnknown, Reason: "NodeStatusUnknown", Message: "Kubelet stopped posting node status.", LastHeartbeatTime: metav1.NewTime(now.Add(-9 * time.Minute))}
	c := top(t, DiagnoseNode(NodeEvidence{Node: node(unknown), Now: now}))
	if !strings.Contains(c.Title, "stopped reporting") || !strings.Contains(strings.Join(c.Evidence, " "), "9m") {
		t.Errorf("unknown: %+v", c)
	}
	stale := corev1.NodeCondition{Type: corev1.NodeReady, Status: corev1.ConditionTrue, LastHeartbeatTime: metav1.NewTime(now.Add(-6 * time.Minute))}
	if c := top(t, DiagnoseNode(NodeEvidence{Node: node(stale), Now: now})); !strings.Contains(c.Title, "stopped reporting") {
		t.Errorf("stale heartbeat: %+v", c)
	}
}

func TestNodeNotReadyHints(t *testing.T) {
	cond := corev1.NodeCondition{Type: corev1.NodeReady, Status: corev1.ConditionFalse, Reason: "KubeletNotReady", Message: "container runtime network not ready: cni plugin not initialized", LastHeartbeatTime: metav1.NewTime(now)}
	c := top(t, DiagnoseNode(NodeEvidence{Node: node(cond), Now: now}))
	if !strings.Contains(c.Hint, "CNI") {
		t.Errorf("hint = %q", c.Hint)
	}
}

func TestNodePressureEventsAndPods(t *testing.T) {
	n := node(healthyReady(), corev1.NodeCondition{Type: corev1.NodeDiskPressure, Status: corev1.ConditionTrue, Reason: "KubeletHasDiskPressure", Message: "low on disk"})
	n.Spec.Taints = []corev1.Taint{{Key: "node.kubernetes.io/disk-pressure", Effect: corev1.TaintEffectNoSchedule}}
	n.Spec.Unschedulable = true
	evs := []events.Row{{Type: "Warning", Reason: "ImageGCFailed", Message: "failed to garbage collect", Count: 3}, {Type: "Normal", Reason: "Starting"}}
	causes := DiagnoseNode(NodeEvidence{Node: n, Events: evs, BadPods: []string{"a/p1: Evicted", "a/p2: Evicted"}, Now: now})
	titles := ""
	for _, c := range causes {
		titles += c.Title + "|"
	}
	for _, want := range []string{"low on disk", "tainted the node", "cannot free", "cordoned", "2 pod(s)"} {
		if !strings.Contains(titles, want) {
			t.Errorf("missing %q in %s", want, titles)
		}
	}
	if !strings.Contains(causes[0].Title, "disk") {
		t.Errorf("disk pressure should rank first: %s", causes[0].Title)
	}
}

func TestCordonDoesNotDuplicateAsTaint(t *testing.T) {
	n := node(healthyReady())
	n.Spec.Unschedulable = true
	n.Spec.Taints = []corev1.Taint{{Key: "node.kubernetes.io/unschedulable", Effect: corev1.TaintEffectNoSchedule}}
	causes := DiagnoseNode(NodeEvidence{Node: n, Now: now})
	if len(causes) != 1 || !strings.Contains(causes[0].Title, "cordoned") {
		t.Errorf("want only the cordon cause, got %+v", causes)
	}
}
