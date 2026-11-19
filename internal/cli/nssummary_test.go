package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/beomzh/kubekaze/internal/term"
)

func nsClusterObjs() []runtime.Object {
	return []runtime.Object{
		crashPod("a", "bad"), healthyPod("a", "good"), healthyPod("b", "ok"),
		ev("a", "bad", "Warning", "BackOff", "x", 5*time.Minute),
		ev("a", "bad", "Warning", "Old", "x", 3*time.Hour),
		ev("b", "ok", "Normal", "Pulled", "x", time.Minute),
	}
}

func forbid(cs *fake.Clientset, resource string) {
	cs.PrependReactor("list", resource, func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: resource}, "", nil)
	})
}

func TestSummarizeNamespaces(t *testing.T) {
	cs := fake.NewSimpleClientset(nsClusterObjs()...)
	s := summarizeNamespaces(context.Background(), cs, []string{"a", "b", "empty"}, "b", time.Now())
	want := []nsSummary{
		{Name: "a", Pods: 2, Unhealthy: 1, Warnings: 1},
		{Name: "b", Current: true, Pods: 1},
		{Name: "empty"},
	}
	for i, w := range want {
		if s.Items[i] != w {
			t.Errorf("item %d = %+v, want %+v", i, s.Items[i], w)
		}
	}
	if len(s.Notes) != 0 {
		t.Errorf("notes = %v", s.Notes)
	}
	// one pods list and one events list, not per namespace
	n := 0
	for _, act := range cs.Actions() {
		if act.GetVerb() == "list" {
			n++
		}
	}
	if n != 2 {
		t.Errorf("list calls = %d, want 2", n)
	}
	if got := s.Items[0].text(); got != "2 pods, 1 unhealthy, 1 warnings" {
		t.Errorf("text = %q", got)
	}
}

func TestSummarizeDegradesOnForbidden(t *testing.T) {
	cs := fake.NewSimpleClientset(nsClusterObjs()...)
	forbid(cs, "pods")
	s := summarizeNamespaces(context.Background(), cs, []string{"a"}, "", time.Now())
	if s.Items[0].Pods != -1 || s.Items[0].Warnings != 1 {
		t.Errorf("item = %+v", s.Items[0])
	}
	if len(s.Notes) != 1 || !strings.Contains(s.Notes[0], "pods not shown") {
		t.Errorf("notes = %v", s.Notes)
	}

	cs = fake.NewSimpleClientset(nsClusterObjs()...)
	forbid(cs, "pods")
	forbid(cs, "events")
	s = summarizeNamespaces(context.Background(), cs, []string{"a", "b"}, "a", time.Now())
	if s.known() || len(s.Notes) != 2 {
		t.Fatalf("known=%v notes=%v", s.known(), s.Notes)
	}
	var out, errb bytes.Buffer
	a := &app{out: &out, err: &errb, pal: term.Palette{}, output: "text"}
	if err := a.printNsList(s, "namespace"); err != nil {
		t.Fatal(err)
	}
	if out.String() != "* a\n  b\n" {
		t.Errorf("plain fallback = %q", out.String())
	}
	if !strings.Contains(errb.String(), "not allowed to list pods cluster-wide") {
		t.Errorf("stderr = %q", errb.String())
	}
}

func TestPrintNsList(t *testing.T) {
	cs := fake.NewSimpleClientset(nsClusterObjs()...)
	s := summarizeNamespaces(context.Background(), cs, []string{"a", "b"}, "b", time.Now())

	var out bytes.Buffer
	a := &app{out: &out, err: &bytes.Buffer{}, pal: term.Palette{}, output: "text"}
	if err := a.printNsList(s, "namespace"); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "  a  2 pods, 1 unhealthy, 1 warnings") || !strings.HasPrefix(lines[1], "* b  1 pods, 0 unhealthy, 0 warnings") {
		t.Errorf("text = %q", out.String())
	}

	out.Reset()
	a.output = "json"
	if err := a.printNsList(s, "namespace"); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Kind  string      `json:"kind"`
		Items []nsSummary `json:"items"`
		Notes []string    `json:"notes"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Kind != "namespace" || len(got.Items) != 2 || got.Items[0].Unhealthy != 1 || !got.Items[1].Current || got.Notes == nil {
		t.Errorf("json = %s", out.String())
	}
}

func TestPickLabelsKeepNameFirst(t *testing.T) {
	s := nsSummaries{Items: []nsSummary{{Name: "a", Pods: 1}, {Name: "payments\x1b[2J", Pods: 3, Unhealthy: 2, Warnings: 4}}}
	labels := s.pickLabels()
	if !strings.HasPrefix(labels[1], "payments") || strings.Contains(labels[1], "\x1b") {
		t.Errorf("label = %q", labels[1])
	}
}
