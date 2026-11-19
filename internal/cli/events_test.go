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

	"github.com/beomzh/kubekaze/internal/events"
	"github.com/beomzh/kubekaze/internal/term"
)

func ev(ns, name, typ, reason, msg string, ago time.Duration) *corev1.Event {
	return &corev1.Event{
		ObjectMeta:     metav1.ObjectMeta{Namespace: ns, Name: name + "." + reason},
		InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: name, Namespace: ns},
		Type:           typ, Reason: reason, Message: msg, Count: 1,
		LastTimestamp: metav1.NewTime(time.Now().Add(-ago)),
	}
}

func TestFetchAndRenderAcrossNamespaces(t *testing.T) {
	cs := fake.NewSimpleClientset(
		ev("a", "api", "Warning", "BackOff", "Back-off restarting", 5*time.Minute),
		ev("b", "db", "Normal", "Pulled", "pulled image", 20*time.Minute),
		ev("b", "db", "Warning", "Failed", "\x1b[2Jevil message", 1*time.Minute),
	)
	rows, _, err := fetchEvents(context.Background(), cs, []string{""}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3", len(rows))
	}
	events.Sort(rows, "time", false)

	var out bytes.Buffer
	a := &app{out: &out, err: &bytes.Buffer{}, pal: term.Palette{}}
	a.printEvents(rows, true, time.Now())
	text := out.String()

	if strings.Contains(text, "\x1b") {
		t.Errorf("terminal escape leaked into output: %q", text)
	}
	if !strings.Contains(text, "\\x1b[2Jevil message") {
		t.Errorf("escaped message missing: %q", text)
	}
	lines := strings.Split(strings.TrimSpace(text), "\n")
	if len(lines) != 4 {
		t.Fatalf("lines = %d, want 4 (header+3)", len(lines))
	}
	if !strings.Contains(lines[1], "Pulled") || !strings.Contains(lines[3], "Failed") {
		t.Errorf("not sorted oldest->newest:\n%s", text)
	}
}

func TestParseFor(t *testing.T) {
	cases := []struct{ in, kind, name string }{
		{"pod/api", "pod", "api"}, {"deploy/web", "deployment", "web"}, {"just-name", "", "just-name"}, {"", "", ""},
	}
	for _, c := range cases {
		if k, n := parseFor(c.in); k != c.kind || n != c.name {
			t.Errorf("parseFor(%q) = %q,%q want %q,%q", c.in, k, n, c.kind, c.name)
		}
	}
}

func TestRunEventsRejectsBadFlags(t *testing.T) {
	a := &app{out: &bytes.Buffer{}, err: &bytes.Buffer{}}
	if err := a.runEvents(context.Background(), eventsFlags{sortKey: "bogus"}); err == nil {
		t.Error("expected error for bad --sort")
	}
	if err := a.runEvents(context.Background(), eventsFlags{sortKey: "time", since: "xyz"}); err == nil {
		t.Error("expected error for bad --since")
	}
}
