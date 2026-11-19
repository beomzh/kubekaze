package rm

import (
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var now = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

func pod(name, node string, phase corev1.PodPhase, reason string, ageH int) corev1.Pod {
	p := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, CreationTimestamp: metav1.NewTime(now.Add(-time.Duration(ageH) * time.Hour))},
		Spec:       corev1.PodSpec{NodeName: node},
		Status:     corev1.PodStatus{Phase: phase},
	}
	if reason == "CrashLoopBackOff" {
		p.Status.ContainerStatuses = []corev1.ContainerStatus{{State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: reason}}}}
	} else {
		p.Status.Reason = reason
	}
	return p
}

func names(ps []corev1.Pod) []string {
	var n []string
	for _, p := range ps {
		n = append(n, p.Name)
	}
	return n
}

func TestSelect(t *testing.T) {
	pods := []corev1.Pod{
		pod("api-1", "n1", corev1.PodRunning, "CrashLoopBackOff", 5),
		pod("api-2", "n2", corev1.PodFailed, "Evicted", 30),
		pod("web-1", "n1", corev1.PodRunning, "CrashLoopBackOff", 1),
		pod("job-1", "n2", corev1.PodSucceeded, "", 50),
	}
	cases := []struct {
		name string
		c    Criteria
		want []string
	}{
		{"glob", Criteria{Patterns: []string{"api-*"}}, []string{"api-1", "api-2"}},
		{"status", Criteria{Statuses: []string{"crashloopbackoff"}}, []string{"api-1", "web-1"}},
		{"multi status", Criteria{Statuses: []string{"Evicted", "Completed"}}, []string{"api-2", "job-1"}},
		{"older", Criteria{OlderThan: 24 * time.Hour, Now: now}, []string{"api-2", "job-1"}},
		{"node", Criteria{Node: "n1"}, []string{"api-1", "web-1"}},
		{"and", Criteria{Patterns: []string{"api-*"}, Statuses: []string{"Evicted"}}, []string{"api-2"}},
		{"none", Criteria{Patterns: []string{"zzz"}}, nil},
	}
	for _, c := range cases {
		got := names(Select(pods, c.c))
		if len(got) != len(c.want) {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%s: got %v want %v", c.name, got, c.want)
			}
		}
	}
}

func TestValidateAndEmpty(t *testing.T) {
	if err := (Criteria{Patterns: []string{"[a-"}}).Validate(); err == nil {
		t.Error("expected invalid pattern error")
	}
	if !(Criteria{}).IsEmpty() || (Criteria{Node: "x"}).IsEmpty() {
		t.Error("IsEmpty wrong")
	}
}
