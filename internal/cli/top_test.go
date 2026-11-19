package cli

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/beomzh/kubekaze/internal/term"
	"github.com/beomzh/kubekaze/internal/ui"
)

const nodeMetrics = `{"items":[{"metadata":{"name":"n1"},"usage":{"cpu":"3500m","memory":"2Gi"}},{"metadata":{"name":"n2"},"usage":{"cpu":"100m","memory":"1Gi"}}]}`
const podMetrics = `{"items":[
 {"metadata":{"name":"web","namespace":"shop"},"containers":[{"name":"c","usage":{"cpu":"450m","memory":"480Mi"}}]},
 {"metadata":{"name":"db","namespace":"data"},"containers":[{"name":"c","usage":{"cpu":"50m","memory":"1Gi"}}]}]}`
const nodeList = `{"kind":"NodeList","apiVersion":"v1","items":[
 {"metadata":{"name":"n1"},"status":{"allocatable":{"cpu":"4","memory":"8Gi"}}},
 {"metadata":{"name":"n2"},"status":{"allocatable":{"cpu":"4","memory":"8Gi"}}}]}`
const podList = `{"kind":"PodList","apiVersion":"v1","items":[
 {"metadata":{"name":"web","namespace":"shop"},"spec":{"containers":[{"name":"c","image":"x","resources":{"limits":{"cpu":"500m","memory":"512Mi"}}}]}}]}`

func topApp(t *testing.T, h http.HandlerFunc) (*app, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	cs, err := kubernetes.NewForConfig(&rest.Config{Host: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	a := &app{out: &out, err: &errb, pal: term.Palette{}, kc: cs, output: "text"}
	a.opts.Namespace = "shop"
	return a, &out, &errb
}

func okHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.URL.Path == "/apis/metrics.k8s.io/v1beta1/nodes":
		w.Write([]byte(nodeMetrics))
	case strings.HasSuffix(r.URL.Path, "/pods") && strings.Contains(r.URL.Path, "metrics.k8s.io"):
		w.Write([]byte(podMetrics))
	case r.URL.Path == "/api/v1/nodes":
		w.Write([]byte(nodeList))
	case strings.HasSuffix(r.URL.Path, "/pods"):
		w.Write([]byte(podList))
	default:
		http.NotFound(w, r)
	}
}

func TestTopDefaultView(t *testing.T) {
	a, out, _ := topApp(t, okHandler)
	if err := a.runTop(context.Background(), topFlags{limit: 10, sortKey: "cpu", interval: time.Second}); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	if strings.Index(s, "n1") > strings.Index(s, "n2") {
		t.Errorf("busiest node first:\n%s", s)
	}
	for _, want := range []string{"88%", "3.50", "web", "90%"} { // n1 cpu 87.5%->88%, web cpu 450/500
		if !strings.Contains(s, want) {
			t.Errorf("missing %q:\n%s", want, s)
		}
	}
}

func TestTopByNamespaceJSON(t *testing.T) {
	a, out, _ := topApp(t, okHandler)
	a.output = "json"
	if err := a.runTop(context.Background(), topFlags{limit: 10, sortKey: "memory", byNamespace: true, interval: time.Second}); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	if !strings.Contains(s, `"namespaces"`) || strings.Contains(s, `"nodes"`) || strings.Index(s, `"data"`) > strings.Index(s, `"shop"`) {
		t.Errorf("json:\n%s", s)
	}
}

func TestTopMetricsServerMissing(t *testing.T) {
	a, out, _ := topApp(t, func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) })
	err := a.runTop(context.Background(), topFlags{limit: 10, sortKey: "cpu", interval: time.Second})
	if err == nil || !strings.Contains(err.Error(), "install metrics-server") || strings.Contains(err.Error(), "\n") {
		t.Errorf("err = %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("unexpected output %q", out.String())
	}
}

func TestTopForbidden(t *testing.T) {
	a, _, _ := topApp(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"kind":"Status","apiVersion":"v1","status":"Failure","reason":"Forbidden","code":403}`))
	})
	err := a.runTop(context.Background(), topFlags{limit: 10, sortKey: "cpu", interval: time.Second})
	if err == nil || !strings.Contains(err.Error(), "metrics.k8s.io") {
		t.Errorf("err = %v", err)
	}
}

func TestTopNodesForbiddenStillShowsPods(t *testing.T) {
	a, out, errb := topApp(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/apis/metrics.k8s.io/v1beta1/nodes" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"kind":"Status","apiVersion":"v1","status":"Failure","reason":"Forbidden","code":403}`))
			return
		}
		okHandler(w, r)
	})
	if err := a.runTop(context.Background(), topFlags{limit: 10, sortKey: "cpu", interval: time.Second}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "web") || !strings.Contains(errb.String(), "node usage (forbidden)") {
		t.Errorf("out=%q err=%q", out.String(), errb.String())
	}
}

func TestTopWatchPlainAndCancel(t *testing.T) {
	a, out, _ := topApp(t, okHandler)
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	err := a.runTop(ctx, topFlags{limit: 10, sortKey: "cpu", watch: true, interval: time.Second})
	if err != ui.ErrCancelled {
		t.Errorf("err = %v", err)
	}
	s := out.String()
	if strings.Contains(s, "\x1b") || strings.Count(s, "----") < 2 {
		t.Errorf("plain repeated output expected:\n%q", s)
	}
}

func TestTopFlagValidation(t *testing.T) {
	var out, errb bytes.Buffer
	if code := Execute([]string{"top", "--sort", "disk"}, strings.NewReader(""), &out, &errb); code == 0 {
		t.Error("want failure for --sort disk")
	}
	if code := Execute([]string{"top", "bogus"}, strings.NewReader(""), &out, &errb); code == 0 {
		t.Error("want failure for unknown arg")
	}
}
