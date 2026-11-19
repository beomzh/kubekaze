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

	"github.com/beomzh/kubekaze/internal/status"
	"github.com/beomzh/kubekaze/internal/term"
)

func TestCollectAndAnalyzeWithFakeCluster(t *testing.T) {
	cs := fake.NewSimpleClientset(crashPod("a", "bad"), healthyPod("a", "good"))
	a := &app{out: &bytes.Buffer{}, err: &bytes.Buffer{}, pal: term.Palette{}}
	in, skipped := a.collect(context.Background(), cs, "a", false)
	if len(skipped) != 0 {
		t.Errorf("skipped: %v", skipped)
	}
	f := status.Analyze(in)
	if len(f) != 1 || f[0].Name != "bad" || f[0].Status != "CrashLoopBackOff" {
		t.Errorf("findings = %+v", f)
	}
}

func TestPrintFindings(t *testing.T) {
	var out bytes.Buffer
	a := &app{out: &out, err: &bytes.Buffer{}, pal: term.Palette{}}
	a.printFindings(nil, "default")
	if !strings.Contains(out.String(), "OK Everything looks healthy in default") {
		t.Errorf("healthy output: %q", out.String())
	}
	out.Reset()
	a.printFindings([]status.Finding{
		{Severity: status.Critical, Kind: "Pod", Namespace: "a", Name: "bad\x1b[2J", Status: "CrashLoopBackOff"},
		{Severity: status.Warning, Kind: "Node", Name: "n1", Status: "SchedulingDisabled"},
	}, "all namespaces")
	text := out.String()
	if strings.Contains(text, "\x1b") {
		t.Errorf("escape leaked: %q", text)
	}
	if strings.Index(text, "Nodes") > strings.Index(text, "Pods") {
		t.Errorf("nodes should be listed before pods:\n%s", text)
	}
	if !strings.Contains(text, "1 critical, 1 warning") {
		t.Errorf("summary missing:\n%s", text)
	}
}

var _ = corev1.Pod{}
var _ = metav1.ObjectMeta{}
var _ = time.Second
