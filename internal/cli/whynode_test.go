package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/beomzh/kubekaze/internal/term"
)

func TestWhyNodeNotReady(t *testing.T) {
	n := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "worker-1", CreationTimestamp: metav1.NewTime(time.Now().Add(-48 * time.Hour))},
		Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{
			{Type: corev1.NodeReady, Status: corev1.ConditionUnknown, Reason: "NodeStatusUnknown", Message: "Kubelet stopped posting node status.", LastHeartbeatTime: metav1.NewTime(time.Now().Add(-12 * time.Minute))},
		}},
	}
	bad := crashPod("a", "stuck")
	bad.Spec.NodeName = "worker-1"
	cs := fake.NewSimpleClientset(n, bad)
	var out bytes.Buffer
	a := &app{out: &out, err: &bytes.Buffer{}, pal: term.Palette{}, output: "text"}
	err := a.whyNode(context.Background(), cs, "worker-1")
	var ec exitCodeError
	if err == nil || !asExit(err, &ec) || ec.code != exitProblems {
		t.Fatalf("a bad node must return exit code %d, got %v", exitProblems, err)
	}
	text := out.String()
	for _, want := range []string{"Node worker-1", "Unknown", "stopped reporting", "systemctl status kubelet", "1 pod(s) on this node are unhealthy"} {
		if !strings.Contains(text, want) {
			t.Errorf("output missing %q:\n%s", want, text)
		}
	}
}

func TestWhyNodeHealthy(t *testing.T) {
	n := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "w"}, Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{
		{Type: corev1.NodeReady, Status: corev1.ConditionTrue, LastHeartbeatTime: metav1.NewTime(time.Now())}}}}
	var out bytes.Buffer
	a := &app{out: &out, err: &bytes.Buffer{}, pal: term.Palette{}, output: "text"}
	if err := a.whyNode(context.Background(), fake.NewSimpleClientset(n), "w"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "No problem found for this node") {
		t.Errorf("output:\n%s", out.String())
	}
}

func asExit(err error, target *exitCodeError) bool {
	ec, ok := err.(exitCodeError)
	if ok {
		*target = ec
	}
	return ok
}

func readyNode(name string) *corev1.Node {
	return &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: name}, Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{
		{Type: corev1.NodeReady, Status: corev1.ConditionTrue, LastHeartbeatTime: metav1.NewTime(time.Now())}}}}
}

func TestIsAllNodes(t *testing.T) {
	for _, s := range []string{"node", "nodes", "no", "Node", "NODES"} {
		if !isAllNodes(s) {
			t.Errorf("%q should mean all nodes", s)
		}
	}
	for _, s := range []string{"node/w1", "pod/node", "node-1", "deploy/node", ""} {
		if isAllNodes(s) {
			t.Errorf("%q must not mean all nodes", s)
		}
	}
}

func TestWhyAllNodesShowsOnlyProblems(t *testing.T) {
	bad := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "worker-2"}, Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{
		{Type: corev1.NodeReady, Status: corev1.ConditionFalse, Reason: "KubeletNotReady", Message: "container runtime is down", LastHeartbeatTime: metav1.NewTime(time.Now())}}}}
	cs := fake.NewSimpleClientset(readyNode("worker-1"), bad, readyNode("worker-3"))
	var out bytes.Buffer
	a := &app{out: &out, err: &bytes.Buffer{}, pal: term.Palette{}, output: "text"}
	err := a.whyAllNodes(context.Background(), cs)
	var ec exitCodeError
	if err == nil || !asExit(err, &ec) || ec.code != exitProblems {
		t.Fatalf("exit code %d expected, got %v", exitProblems, err)
	}
	text := out.String()
	for _, want := range []string{"Node worker-2", "container runtime is down", "1 of 3 node(s) have problems", "Healthy: worker-1, worker-3"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "Node worker-1") || strings.Contains(text, "Node worker-3") {
		t.Errorf("healthy nodes must only be summarised:\n%s", text)
	}
}

func TestWhyAllNodesHealthyAndJSON(t *testing.T) {
	cs := fake.NewSimpleClientset(readyNode("a"), readyNode("b"))
	var out bytes.Buffer
	a := &app{out: &out, err: &bytes.Buffer{}, pal: term.Palette{}, output: "text"}
	if err := a.whyAllNodes(context.Background(), cs); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "All 2 node(s) look healthy: a, b") {
		t.Errorf("output: %s", out.String())
	}
	out.Reset()
	a.output = "json"
	if err := a.whyAllNodes(context.Background(), cs); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(strings.TrimSpace(out.String()), "[") || !strings.Contains(out.String(), `"node": "a"`) {
		t.Errorf("json: %s", out.String())
	}
}

func TestWhyTargetCompletionOffersNodes(t *testing.T) {
	cs := fake.NewSimpleClientset(readyNode("worker-1"), readyNode("worker-2"), healthyPod("default", "api-1"))
	a := &app{out: &bytes.Buffer{}, err: &bytes.Buffer{}, pal: term.Palette{}, kc: cs}
	a.opts.Namespace = "default"

	top, _ := a.targetNames(nil, "", true, true)
	joined := strings.Join(top, " ")
	for _, want := range []string{"api-1", "deploy/", "sts/", "ds/", "job/", "node/", "nodes"} {
		if !strings.Contains(joined, want) {
			t.Errorf("why completion should offer %q, got %v", want, top)
		}
	}
	nodes, _ := a.targetNames(nil, "node/", true, true)
	if strings.Join(nodes, " ") != "node/worker-1 node/worker-2" {
		t.Errorf("node names: %v", nodes)
	}
	// sh and logs do not take nodes
	shTop, _ := a.targetNames(nil, "", true, false)
	if strings.Contains(strings.Join(shTop, " "), "node/") {
		t.Errorf("sh/logs completion must not offer nodes: %v", shTop)
	}
	if n, _ := a.targetNames(nil, "node/", true, false); len(n) != 0 {
		t.Errorf("sh/logs must not complete node names: %v", n)
	}
}

// Regression for a real report: with "why nodes" a healthy node was called
// silent because its heartbeat was 4 minutes old, and another node was blamed
// on a 15 day old ImageGCFailed event while the real lead (a failing
// ovnkube-node pod and the pods stuck behind it) was ranked below it.
func TestWhyNodesRealWorldReport(t *testing.T) {
	now := time.Now()
	node := func(name string, hbAge time.Duration) *corev1.Node {
		return &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: name, CreationTimestamp: metav1.NewTime(now.Add(-72 * 24 * time.Hour))},
			Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{
				{Type: corev1.NodeReady, Status: corev1.ConditionTrue, Reason: "KubeletReady", LastHeartbeatTime: metav1.NewTime(now.Add(-hbAge))}}}}
	}
	stuck := func(ns, name, nodeName string) *corev1.Pod {
		return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name, CreationTimestamp: metav1.NewTime(now.Add(-3 * time.Hour))},
			Spec: corev1.PodSpec{NodeName: nodeName}, Status: corev1.PodStatus{Phase: corev1.PodPending,
				ContainerStatuses: []corev1.ContainerStatus{{State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ContainerCreating"}}}}}}
	}
	ovn := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: "openshift-ovn-kubernetes", Name: "ovnkube-node-gwqdl", CreationTimestamp: metav1.NewTime(now.Add(-40 * 24 * time.Hour))},
		Spec: corev1.PodSpec{NodeName: "worker-1"}, Status: corev1.PodStatus{Phase: corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{{State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CreateContainerError"}}}}}}
	oldEvent := &corev1.Event{
		ObjectMeta:     metav1.ObjectMeta{Namespace: "default", Name: "gc"},
		InvolvedObject: corev1.ObjectReference{Kind: "Node", Name: "worker-1"},
		Type:           "Warning", Reason: "ImageGCFailed", Count: 1,
		Message:       "rpc error: code = NotFound desc = could not find container: ID does not exist",
		LastTimestamp: metav1.NewTime(now.Add(-15 * 24 * time.Hour)),
	}
	cs := fake.NewSimpleClientset(
		node("aio-2", 4*time.Minute+10*time.Second), // heartbeat 4 minutes old: normal
		node("worker-1", 98*time.Second), node("worker-2", 98*time.Second),
		ovn, stuck("openshift-monitoring", "metrics-server-6d76ffbf88-6rntd", "worker-1"), stuck("openshift-image-registry", "image-pruner-29898720-b8dws", "worker-1"),
		oldEvent,
	)
	var out bytes.Buffer
	a := &app{out: &out, err: &bytes.Buffer{}, pal: term.Palette{}, output: "text"}
	err := a.whyAllNodes(context.Background(), cs)
	var ec exitCodeError
	if err == nil || !asExit(err, &ec) {
		t.Fatalf("worker-1 has a real problem, exit code expected: %v", err)
	}
	text := out.String()
	if strings.Contains(text, "stopped reporting") || strings.Contains(text, "Node aio-2") {
		t.Errorf("a Ready node with a 4 minute heartbeat must not be flagged:\n%s", text)
	}
	if strings.Contains(text, "cannot free enough resources") {
		t.Errorf("a 15 day old GC warning must not be a cause:\n%s", text)
	}
	causes := text[strings.Index(text, "Suspected causes"):]
	first := causes[:strings.Index(causes, "\n2.")]
	if !strings.Contains(first, "network (CNI) agent") || !strings.Contains(first, "ovnkube-node-gwqdl") {
		t.Errorf("the failing CNI agent must be cause number 1:\n%s", text)
	}
	if !strings.Contains(text, "1 of 3 node(s) have problems") || !strings.Contains(text, "Healthy: aio-2, worker-2") {
		t.Errorf("summary wrong:\n%s", text)
	}
}
