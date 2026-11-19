package cli

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestLineLevel(t *testing.T) {
	cases := map[string]string{
		"ERROR: connection refused": "error", "panic: nil pointer": "error", "request failed": "error",
		"WARN slow query": "warn", "warning: deprecated": "warn", "GET /healthz 200": "",
		"terror alert": "", // "err" inside a word must not match
	}
	for in, want := range cases {
		if got := lineLevel(in); got != want {
			t.Errorf("lineLevel(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLogSourcesIncludePreviousFirst(t *testing.T) {
	restarted := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "p1"},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "app"}, {Name: "side"}}},
		Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{
			{Name: "app", RestartCount: 2, LastTerminationState: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 1}}},
			{Name: "side"},
		}},
	}
	src := logSources([]corev1.Pod{restarted}, logsFlags{})
	if len(src) != 3 || !src[0].previous || src[0].container != "app" || src[1].previous || src[1].container != "app" || src[2].container != "side" {
		t.Fatalf("sources = %+v", src)
	}
	if got := logSources([]corev1.Pod{restarted}, logsFlags{noPrevious: true}); len(got) != 2 {
		t.Errorf("--no-previous should drop previous runs, got %d", len(got))
	}
	if got := logSources([]corev1.Pod{restarted}, logsFlags{container: "side"}); len(got) != 1 || got[0].container != "side" {
		t.Errorf("-c filter wrong: %+v", got)
	}
}
