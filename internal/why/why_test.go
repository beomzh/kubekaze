package why

import (
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/beomzh/kubekaze/internal/events"
)

var now = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

func podWith(cs corev1.ContainerStatus) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "a"},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Resources: corev1.ResourceRequirements{
			Limits: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("128Mi")}}}}},
		Status: corev1.PodStatus{Phase: corev1.PodRunning, ContainerStatuses: []corev1.ContainerStatus{cs}},
	}
}

func top(t *testing.T, causes []Cause) Cause {
	t.Helper()
	if len(causes) == 0 {
		t.Fatal("no causes")
	}
	return causes[0]
}

func TestHealthyPodHasNoCause(t *testing.T) {
	p := podWith(corev1.ContainerStatus{Name: "app", Ready: true, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}})
	if c := Diagnose(Evidence{Pod: p, Now: now}); c != nil {
		t.Errorf("got %+v", c)
	}
}

func TestOOMKilled(t *testing.T) {
	p := podWith(corev1.ContainerStatus{Name: "app", RestartCount: 3,
		State:                corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}},
		LastTerminationState: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{Reason: "OOMKilled", ExitCode: 137}}})
	c := top(t, Diagnose(Evidence{Pod: p, Now: now}))
	if !strings.Contains(c.Title, "memory limit") || !strings.Contains(strings.Join(c.Evidence, " "), "128Mi") {
		t.Errorf("OOM cause wrong: %+v", c)
	}
}

func TestCrashWithExitCodeShowsLogs(t *testing.T) {
	p := podWith(corev1.ContainerStatus{Name: "app", RestartCount: 2,
		State:                corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}},
		LastTerminationState: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{Reason: "Error", ExitCode: 1}}})
	causes := Diagnose(Evidence{Pod: p, LogTail: []string{"starting", "FATAL: cannot connect to db"}, Now: now})
	c := top(t, causes)
	if !strings.Contains(c.Title, "exit code 1") || !strings.Contains(strings.Join(c.Evidence, "\n"), "cannot connect to db") {
		t.Errorf("cause = %+v", c)
	}
	for _, x := range causes {
		if strings.Contains(x.Title, "keeps crashing") {
			t.Error("generic crash cause should be suppressed when a specific one exists")
		}
	}
}

func TestImagePullHints(t *testing.T) {
	mk := func(msg string) []Cause {
		p := podWith(corev1.ContainerStatus{Name: "app", Image: "reg/x:1",
			State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ImagePullBackOff", Message: msg}}})
		return Diagnose(Evidence{Pod: p, Now: now})
	}
	if c := top(t, mk("manifest unknown")); !strings.Contains(c.Hint, "does not exist") {
		t.Errorf("hint: %s", c.Hint)
	}
	if c := top(t, mk("pull access denied, unauthorized")); !strings.Contains(c.Hint, "imagePullSecrets") {
		t.Errorf("hint: %s", c.Hint)
	}
}

func TestSchedulingInsufficient(t *testing.T) {
	p := &corev1.Pod{Status: corev1.PodStatus{Phase: corev1.PodPending, Conditions: []corev1.PodCondition{
		{Type: corev1.PodScheduled, Status: corev1.ConditionFalse, Reason: "Unschedulable", Message: "0/3 nodes are available: 3 Insufficient memory."}}}}
	c := top(t, Diagnose(Evidence{Pod: p, Now: now}))
	if !strings.Contains(c.Title, "enough memory") {
		t.Errorf("cause = %+v", c)
	}
}

func TestProbeEventAndRollout(t *testing.T) {
	p := podWith(corev1.ContainerStatus{Name: "app", Ready: false, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}})
	evs := []events.Row{{Type: "Warning", Reason: "Unhealthy", Message: "Readiness probe failed: HTTP probe failed with statuscode: 503", Count: 4}}
	causes := Diagnose(Evidence{Pod: p, Events: evs, RolloutAge: 5 * time.Minute, Now: now})
	if len(causes) != 2 || !strings.Contains(causes[0].Title, "readiness probe") || !strings.Contains(causes[1].Title, "rollout") {
		t.Errorf("causes = %+v", causes)
	}
}

func TestStuckTerminating(t *testing.T) {
	d := metav1.NewTime(now.Add(-10 * time.Minute))
	p := podWith(corev1.ContainerStatus{Name: "app", State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}})
	p.DeletionTimestamp = &d
	p.Finalizers = []string{"example.com/protect"}
	c := top(t, Diagnose(Evidence{Pod: p, Now: now}))
	if !strings.Contains(c.Title, "stuck terminating") {
		t.Errorf("cause = %+v", c)
	}
}
