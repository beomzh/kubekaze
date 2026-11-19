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

// The message below is the real error from an OpenShift node whose container
// storage returned I/O errors. It used to be reported as "missing ConfigMap".
const overlayIOError = `failed to mount container k8s_ovnkube-controller_ovnkube-node-gwqdl_openshift-ovn-kubernetes_a8fa7790_2(f35d7dc4): creating overlay mount to /var/lib/containers/storage/overlay/86e74a92/merged, mount_data="lowerdir=/var/lib/containers/storage/overlay/l/SF27MLJUOLXVYWZGLATMDHWZ7G:/var/lib/containers/storage/overlay/l/DVH4VWICPKMGJAGVPFSATFQGEW,upperdir=/var/lib/containers/storage/overlay/86e74a92/diff,workdir=/var/lib/containers/storage/overlay/86e74a92/work,volatile,context=\"system_u:object_r:container_file_t:s0:c351,c793\"": input/output error`

func TestStorageIOErrorIsNotAConfigProblem(t *testing.T) {
	p := podWith(corev1.ContainerStatus{Name: "app", State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{
		Reason: "CreateContainerError", Message: "Error: " + overlayIOError}}})
	c := top(t, Diagnose(Evidence{Pod: p, Now: now}))
	if !strings.Contains(c.Title, "container storage failed") {
		t.Fatalf("title = %q", c.Title)
	}
	if strings.Contains(c.Title+c.Hint, "ConfigMap") {
		t.Errorf("must not point at ConfigMaps: %+v", c)
	}
	if len(c.Evidence[0]) > maxEvidence+len("CreateContainerError: ")+4 {
		t.Errorf("evidence not shortened: %d chars", len(c.Evidence[0]))
	}
}

func TestStorageIOErrorFromEventsToo(t *testing.T) {
	p := podWith(corev1.ContainerStatus{Name: "app"})
	p.Status.Phase = corev1.PodPending
	c := top(t, Diagnose(Evidence{Pod: p, Now: now, Events: []events.Row{{Type: "Warning", Reason: "Failed", Message: overlayIOError, Count: 3}}}))
	if !strings.Contains(c.Title, "container storage failed") {
		t.Errorf("got %q", c.Title)
	}
}

func TestMissingConfigStillReportedAsConfig(t *testing.T) {
	p := podWith(corev1.ContainerStatus{Name: "app", State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{
		Reason: "CreateContainerConfigError", Message: `secret "db" not found`}}})
	if c := top(t, Diagnose(Evidence{Pod: p, Now: now})); !strings.Contains(c.Title, "ConfigMap/Secret") {
		t.Errorf("got %q", c.Title)
	}
}

func TestLogLinesOnlyForTheirOwnContainer(t *testing.T) {
	p := podWith(corev1.ContainerStatus{Name: "app", LastTerminationState: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{Reason: "Error", ExitCode: 1}}})
	other := Diagnose(Evidence{Pod: p, Now: now, LogTail: []string{"from the sidecar"}, LogContainer: "sidecar"})
	if strings.Contains(strings.Join(other[0].Evidence, "|"), "sidecar") {
		t.Errorf("logs of another container quoted: %v", other[0].Evidence)
	}
	own := Diagnose(Evidence{Pod: p, Now: now, LogTail: []string{"boom"}, LogContainer: "app"})
	if !strings.Contains(strings.Join(own[0].Evidence, "|"), "boom") {
		t.Errorf("own logs missing: %v", own[0].Evidence)
	}
}

func TestShorten(t *testing.T) {
	if got := Shorten("short", 10); got != "short" {
		t.Errorf("got %q", got)
	}
	if got := Shorten("한글로된아주긴메시지입니다", 5); got != "한글로된…" {
		t.Errorf("got %q", got)
	}
}
