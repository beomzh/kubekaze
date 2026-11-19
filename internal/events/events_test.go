package events

import (
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var t0 = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

func row(ns, typ, reason, name, msg string, count int, lastMinAgo int) Row {
	last := t0.Add(-time.Duration(lastMinAgo) * time.Minute)
	return Row{Namespace: ns, Type: typ, Reason: reason, Kind: "Pod", Name: name, Message: msg, Count: count, First: last, Last: last, Merged: 1}
}

func TestSortTimeNewestLast(t *testing.T) {
	rows := []Row{row("a", "Normal", "X", "p1", "m", 1, 1), row("a", "Normal", "X", "p2", "m", 1, 30), row("a", "Normal", "X", "p3", "m", 1, 10)}
	Sort(rows, "time", false)
	if rows[0].Name != "p2" || rows[2].Name != "p1" {
		t.Errorf("order = %s,%s,%s", rows[0].Name, rows[1].Name, rows[2].Name)
	}
	Sort(rows, "time", true)
	if rows[0].Name != "p1" {
		t.Errorf("reverse order wrong: %s first", rows[0].Name)
	}
}

func TestSortTypeWarningsFirst(t *testing.T) {
	rows := []Row{row("a", "Normal", "X", "p1", "m", 1, 1), row("a", "Warning", "Y", "p2", "m", 1, 5)}
	Sort(rows, "type", false)
	if rows[0].Type != "Warning" {
		t.Errorf("warning should come first, got %s", rows[0].Type)
	}
}

func TestSortCountAndNamespace(t *testing.T) {
	rows := []Row{row("b", "Normal", "X", "p1", "m", 5, 1), row("a", "Normal", "X", "p2", "m", 9, 2)}
	Sort(rows, "count", true)
	if rows[0].Count != 9 {
		t.Error("count reverse should put 9 first")
	}
	Sort(rows, "namespace", false)
	if rows[0].Namespace != "a" {
		t.Error("namespace sort wrong")
	}
}

func TestCollapse(t *testing.T) {
	rows := []Row{
		row("a", "Warning", "BackOff", "p1", "restarting", 3, 20),
		row("a", "Warning", "BackOff", "p1", "restarting", 4, 5),
		row("a", "Warning", "BackOff", "p1", "other msg", 1, 5),
	}
	out := Collapse(rows)
	if len(out) != 2 {
		t.Fatalf("len = %d, want 2", len(out))
	}
	if out[0].Count != 7 || out[0].Merged != 2 {
		t.Errorf("count=%d merged=%d, want 7/2", out[0].Count, out[0].Merged)
	}
	if !out[0].First.Equal(t0.Add(-20*time.Minute)) || !out[0].Last.Equal(t0.Add(-5*time.Minute)) {
		t.Errorf("time range not widened: %v - %v", out[0].First, out[0].Last)
	}
}

func TestFilter(t *testing.T) {
	rows := []Row{
		row("a", "Warning", "BackOff", "p1", "m", 1, 5),
		row("a", "Normal", "Pulled", "p1", "m", 1, 5),
		row("a", "Warning", "FailedScheduling", "p2", "m", 1, 90),
	}
	got := Filter{Types: []string{"warning"}, Since: time.Hour, Now: t0}.Apply(rows)
	if len(got) != 1 || got[0].Reason != "BackOff" {
		t.Errorf("filter got %+v", got)
	}
	got = Filter{Kind: "pod", Name: "p2"}.Apply(rows)
	if len(got) != 1 || got[0].Name != "p2" {
		t.Errorf("for-filter got %+v", got)
	}
	got = Filter{Reasons: []string{"pulled"}}.Apply(rows)
	if len(got) != 1 {
		t.Errorf("reason filter got %+v", got)
	}
}

func TestSummarize(t *testing.T) {
	rows := []Row{
		row("a", "Warning", "BackOff", "p1", "m", 10, 5),
		row("a", "Warning", "Failed", "p2", "m", 2, 3),
		row("a", "Normal", "Pulled", "p1", "m", 1, 1),
		row("b", "Warning", "BackOff", "p3", "m", 30, 2),
		row("c", "Normal", "Pulled", "p4", "m", 1, 1),
	}
	s := Summarize(rows)
	if len(s) != 3 || s[0].Namespace != "b" || s[1].Namespace != "a" || s[2].Namespace != "c" {
		t.Fatalf("order = %+v", s)
	}
	if s[1].Warnings != 12 || s[1].Events != 13 || s[1].TopReasons[0] != "BackOff" {
		t.Errorf("summary a = %+v", s[1])
	}
}

func TestFromCore(t *testing.T) {
	ev := corev1.Event{
		ObjectMeta:     metav1.ObjectMeta{Namespace: "ns1", CreationTimestamp: metav1.NewTime(t0)},
		InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: "web"},
		Type:           "Warning", Reason: "BackOff", Message: " Back-off restarting \n",
		Count: 0, LastTimestamp: metav1.NewTime(t0.Add(time.Minute)),
	}
	r := FromCore(ev)
	if r.Count != 1 || r.Message != "Back-off restarting" || !r.Last.Equal(t0.Add(time.Minute)) || r.Object() != "pod/web" {
		t.Errorf("FromCore = %+v", r)
	}
}

func TestParseSinceAndAge(t *testing.T) {
	if d, err := ParseSince("2d"); err != nil || d != 48*time.Hour {
		t.Errorf("ParseSince(2d) = %v %v", d, err)
	}
	if d, err := ParseSince("30m"); err != nil || d != 30*time.Minute {
		t.Errorf("ParseSince(30m) = %v %v", d, err)
	}
	if _, err := ParseSince("zzz"); err == nil {
		t.Error("expected error")
	}
	for in, want := range map[time.Duration]string{30 * time.Second: "30s", 10 * time.Minute: "10m", 5 * time.Hour: "5h", 72 * time.Hour: "3d"} {
		if got := Age(in); got != want {
			t.Errorf("Age(%v) = %s, want %s", in, got, want)
		}
	}
}
