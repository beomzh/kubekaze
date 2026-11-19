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
