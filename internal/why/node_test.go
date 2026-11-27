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
	stale := corev1.NodeCondition{Type: corev1.NodeReady, Status: corev1.ConditionTrue, LastHeartbeatTime: metav1.NewTime(now.Add(-12 * time.Minute))}
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
	evs := []events.Row{{Type: "Warning", Reason: "ImageGCFailed", Message: "failed to garbage collect required amount of images", Count: 3, Last: now.Add(-10 * time.Minute)}, {Type: "Normal", Reason: "Starting"}}
	causes := DiagnoseNode(NodeEvidence{Node: n, Events: evs, BadPods: []NodePod{{Namespace: "a", Name: "p1", Status: "Evicted"}, {Namespace: "a", Name: "p2", Status: "Evicted"}}, Now: now})
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

func TestNormalHeartbeatAgeIsNotAProblem(t *testing.T) {
	// Since Kubernetes 1.17 the node status (and its heartbeat time) is written
	// every ~5 minutes when nothing changes: 4 minutes old is normal.
	cond := corev1.NodeCondition{Type: corev1.NodeReady, Status: corev1.ConditionTrue, Reason: "KubeletReady", LastHeartbeatTime: metav1.NewTime(now.Add(-4*time.Minute - 30*time.Second))}
	if c := DiagnoseNode(NodeEvidence{Node: node(cond), Now: now}); c != nil {
		t.Errorf("a Ready node with a 4m30s old heartbeat must be healthy, got %+v", c)
	}
}

func TestOldEventsDoNotCauseAnything(t *testing.T) {
	old := []events.Row{{Type: "Warning", Reason: "SystemOOM", Message: "System OOM encountered", Count: 2, Last: now.Add(-15 * 24 * time.Hour)}}
	if c := DiagnoseNode(NodeEvidence{Node: node(healthyReady()), Events: old, Now: now}); c != nil {
		t.Errorf("a two week old warning must not flag a healthy node: %+v", c)
	}
	recent := []events.Row{{Type: "Warning", Reason: "SystemOOM", Message: "System OOM encountered", Count: 2, Last: now.Add(-10 * time.Minute)}}
	if c := top(t, DiagnoseNode(NodeEvidence{Node: node(healthyReady()), Events: recent, Now: now})); !strings.Contains(c.Title, "system OOM") {
		t.Errorf("a recent one must: %+v", c)
	}
}

func TestImageGCNotFoundIsLowPriorityNoise(t *testing.T) {
	msg := `get container status: runtime container status: rpc error: code = NotFound desc = could not find container "73fc": container with ID starting with 73fc not found: ID does not exist`
	evs := []events.Row{{Type: "Warning", Reason: "ImageGCFailed", Message: msg, Count: 1, Last: now.Add(-5 * time.Minute)}}
	causes := DiagnoseNode(NodeEvidence{Node: node(healthyReady()), Events: evs, Now: now})
	if len(causes) != 1 || causes[0].Score > 30 || strings.Contains(causes[0].Title, "cannot free") || strings.Contains(causes[0].Hint, "reclaim space") {
		t.Errorf("a container-not-found GC error is noise, not a disk problem: %+v", causes)
	}
}

func TestFailingCNIAgentExplainsStuckPods(t *testing.T) {
	bad := []NodePod{
		{Namespace: "openshift-image-registry", Name: "image-pruner-29898720-b8dws", Status: "ContainerCreating", Age: 3 * time.Hour},
		{Namespace: "openshift-monitoring", Name: "metrics-server-6d76ffbf88-6rntd", Status: "ContainerCreating", Age: 3 * time.Hour},
		{Namespace: "openshift-ovn-kubernetes", Name: "ovnkube-node-gwqdl", Status: "CreateContainerError", Age: 40 * 24 * time.Hour},
	}
	causes := DiagnoseNode(NodeEvidence{Node: node(healthyReady()), BadPods: bad, Now: now})
	c := top(t, causes)
	if !strings.Contains(c.Title, "network (CNI) agent") || !strings.Contains(strings.Join(c.Evidence, "\n"), "ovnkube-node-gwqdl") ||
		!strings.Contains(strings.Join(c.Evidence, "\n"), "2 other pod(s)") || !strings.Contains(c.Hint, "kubekaze why ovnkube-node-gwqdl -n openshift-ovn-kubernetes") {
		t.Errorf("the CNI agent must be the top cause and point at the failing pod: %+v", c)
	}
}

func TestStuckContainerCreatingWithoutCNIPod(t *testing.T) {
	bad := []NodePod{{Namespace: "a", Name: "x", Status: "ContainerCreating", Age: time.Hour}, {Namespace: "a", Name: "y", Status: "ContainerCreating", Age: time.Hour}}
	c := top(t, DiagnoseNode(NodeEvidence{Node: node(healthyReady()), BadPods: bad, Now: now}))
	if !strings.Contains(c.Title, "stuck in ContainerCreating") {
		t.Errorf("%+v", c)
	}
	one := bad[:1]
	if c := DiagnoseNode(NodeEvidence{Node: node(healthyReady()), BadPods: one, Now: now}); c != nil {
		t.Errorf("a single slow pod alone is not a node problem: %+v", c)
	}
}

func TestCNIPodNamePattern(t *testing.T) {
	for _, n := range []string{"ovnkube-node-gwqdl", "calico-node-x7k2p", "cilium-abcde", "aws-node-4kq8z", "kube-flannel-ds-9xh2q", "multus-additional-cni-plugins-abcde"} {
		if !cniPod.MatchString(n) {
			t.Errorf("%s should be a CNI agent", n)
		}
	}
	for _, n := range []string{"api-7d9f", "node-exporter-abcde", "ovnkube-control-plane-xyz12", "nginx"} {
		if cniPod.MatchString(n) {
			t.Errorf("%s must not be treated as a CNI agent", n)
		}
	}
}

func restartAt(ns, pod string, count int32, ago time.Duration) PodRestart {
	return PodRestart{Namespace: ns, Pod: pod, Container: "c", Count: count, Last: now.Add(-ago)}
}

func TestRestartBurstPointsAtTheNode(t *testing.T) {
	rs := []PodRestart{
		restartAt("a", "p1", 1, 3*time.Hour), restartAt("b", "p2", 1, 3*time.Hour+2*time.Minute),
		restartAt("c", "p3", 2, 3*time.Hour+4*time.Minute), restartAt("d", "p4", 1, 3*time.Hour+6*time.Minute),
		restartAt("e", "far", 1, 40*time.Hour), // not part of the burst
	}
	causes := DiagnoseNode(NodeEvidence{Node: node(healthyReady()), Restarts: rs, Now: now})
	if len(causes) == 0 || !strings.Contains(causes[0].Title, "4 pods on this node restarted") {
		t.Fatalf("got %+v", causes)
	}
	if !strings.Contains(causes[0].Hint, "coredumpctl") {
		t.Errorf("hint should send the reader into the node: %q", causes[0].Hint)
	}
}

func TestRestartsSpreadOverTimeAreNotABurst(t *testing.T) {
	var rs []PodRestart
	for i := 0; i < 6; i++ {
		rs = append(rs, restartAt("a", "p"+string(rune('1'+i)), 1, time.Duration(i+1)*5*time.Hour))
	}
	if c := DiagnoseNode(NodeEvidence{Node: node(healthyReady()), Restarts: rs, Now: now}); c != nil {
		t.Errorf("got %+v", c)
	}
}

func TestFrequentRestarterIsListedWorstFirst(t *testing.T) {
	rs := []PodRestart{restartAt("a", "small", 12, time.Hour), restartAt("b", "big", 450, 80*time.Hour), restartAt("c", "calm", 3, time.Hour)}
	if c := DiagnoseNode(NodeEvidence{Node: node(healthyReady()), Restarts: rs, Now: now}); c != nil {
		t.Fatalf("a restarting pod alone must not make the node unhealthy: %+v", c)
	}
	cordoned := node(healthyReady())
	cordoned.Spec.Unschedulable = true
	causes := DiagnoseNode(NodeEvidence{Node: cordoned, Restarts: rs, Now: now})
	last := causes[len(causes)-1]
	if !strings.Contains(last.Title, "2 container(s)") || !strings.HasPrefix(last.Evidence[0], "b/big") {
		t.Fatalf("got %+v", causes)
	}
}

func TestOldRestartsAreHistory(t *testing.T) {
	rs := []PodRestart{restartAt("a", "p", 900, 9*24*time.Hour)}
	if c := DiagnoseNode(NodeEvidence{Node: node(healthyReady()), Restarts: rs, Now: now}); c != nil {
		t.Errorf("got %+v", c)
	}
}

func TestRebootStaysVisibleForAWeekButRanksLower(t *testing.T) {
	ev := func(ago time.Duration) []events.Row {
		return []events.Row{{Type: "Warning", Reason: "Rebooted", Message: "Node n1 has been rebooted", Last: now.Add(-ago)}}
	}
	c := top(t, DiagnoseNode(NodeEvidence{Node: node(healthyReady()), Events: ev(3 * 24 * time.Hour), Now: now}))
	if !strings.Contains(c.Title, "rebooted") || c.Score >= 60 {
		t.Errorf("3d old: %+v", c)
	}
	if c := DiagnoseNode(NodeEvidence{Node: node(healthyReady()), Events: ev(9 * 24 * time.Hour), Now: now}); c != nil {
		t.Errorf("9d old should be ignored: %+v", c)
	}
}

func cpBurst(ago time.Duration, ns string) []PodRestart {
	var rs []PodRestart
	for i, p := range []string{"kube-controller-manager-n1", "kube-scheduler-n1", "cloud-controller-manager-n1", "snapshot-controller-x"} {
		r := restartAt(ns, p, 11, ago+time.Duration(i)*time.Minute)
		r.ExitCode, r.Reason = 1, "Error"
		rs = append(rs, r)
	}
	return rs
}

func TestOldBurstIsPastAndPullsInNothingElse(t *testing.T) {
	causes := DiagnoseNode(NodeEvidence{Node: node(healthyReady()), Restarts: cpBurst(6*24*time.Hour, "kube-system"),
		BadPods: []NodePod{{Namespace: "a", Name: "p", Status: "Error"}}, Now: now})
	if len(causes) != 1 || !causes[0].Past {
		t.Fatalf("want a single past cause, got %+v", causes)
	}
}

func TestRecentBurstShowsTimeExitReasonAndControlPlaneHint(t *testing.T) {
	c := top(t, DiagnoseNode(NodeEvidence{Node: node(healthyReady()), Restarts: cpBurst(2*time.Hour, "kube-system"), Now: now}))
	all := strings.Join(c.Evidence, "\n")
	want := now.Add(-2 * time.Hour).Local().Format("2006-01-02 15:04:05")
	if c.Past || !strings.Contains(all, want) || !strings.Contains(all, "exit 1 Error") || !strings.Contains(c.Hint, "etcd") {
		t.Errorf("%+v", c)
	}
}

func TestApplicationPodsNamedLikeControlPlaneAreNotControlPlane(t *testing.T) {
	c := top(t, DiagnoseNode(NodeEvidence{Node: node(healthyReady()), Restarts: cpBurst(time.Hour, "shop"), Now: now}))
	if !strings.Contains(c.Hint, "nothing to do with each other") {
		t.Errorf("hint = %q", c.Hint)
	}
}
