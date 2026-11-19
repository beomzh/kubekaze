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
