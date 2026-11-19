package podstatus

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func withContainer(phase corev1.PodPhase, st corev1.ContainerState, ready bool) *corev1.Pod {
	return &corev1.Pod{Status: corev1.PodStatus{
		Phase:             phase,
		ContainerStatuses: []corev1.ContainerStatus{{State: st, Ready: ready}},
	}}
}

func TestOf(t *testing.T) {
	now := metav1.Now()
	cases := []struct {
		name string
		pod  *corev1.Pod
		want string
	}{
		{"running", withContainer(corev1.PodRunning, corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}, true), "Running"},
		{"crashloop", withContainer(corev1.PodRunning, corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}}, false), "CrashLoopBackOff"},
		{"imagepull", withContainer(corev1.PodPending, corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ImagePullBackOff"}}, false), "ImagePullBackOff"},
		{"oom", withContainer(corev1.PodRunning, corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{Reason: "OOMKilled", ExitCode: 137}}, false), "OOMKilled"},
		{"completed", withContainer(corev1.PodSucceeded, corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{Reason: "Completed"}}, false), "Completed"},
		{"error exit", withContainer(corev1.PodFailed, corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 2}}, false), "ExitCode:2"},
		{"evicted", &corev1.Pod{Status: corev1.PodStatus{Phase: corev1.PodFailed, Reason: "Evicted"}}, "Evicted"},
		{"pending", &corev1.Pod{Status: corev1.PodStatus{Phase: corev1.PodPending}}, "Pending"},
		{"terminating", func() *corev1.Pod {
			p := withContainer(corev1.PodRunning, corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}, true)
			p.DeletionTimestamp = &now
			return p
		}(), "Terminating"},
		{"init failing", &corev1.Pod{
			Spec: corev1.PodSpec{InitContainers: []corev1.Container{{Name: "i"}}},
			Status: corev1.PodStatus{Phase: corev1.PodPending, InitContainerStatuses: []corev1.ContainerStatus{
				{State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}}}}},
		}, "Init:CrashLoopBackOff"},
	}
	for _, c := range cases {
		if got := Of(c.pod); got != c.want {
			t.Errorf("%s: Of() = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestHealthyAndRestarts(t *testing.T) {
	ok := withContainer(corev1.PodRunning, corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}, true)
	if !Healthy(ok) {
		t.Error("running+ready should be healthy")
	}
	notReady := withContainer(corev1.PodRunning, corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}, false)
	if Healthy(notReady) {
		t.Error("running but not ready should be unhealthy")
	}
	ok.Status.ContainerStatuses[0].RestartCount = 4
	if Restarts(ok) != 4 {
		t.Error("restarts wrong")
	}
}
