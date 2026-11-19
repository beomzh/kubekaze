package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/beomzh/kubekaze/internal/term"
)

func oomPod() *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: "a", Name: "oom", UID: types.UID("u"), CreationTimestamp: metav1.NewTime(time.Now().Add(-time.Hour))},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "app"}}},
		Status: corev1.PodStatus{Phase: corev1.PodRunning, ContainerStatuses: []corev1.ContainerStatus{{
			Name: "app", RestartCount: 4,
			State:                corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}},
			LastTerminationState: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{Reason: "OOMKilled", ExitCode: 137, FinishedAt: metav1.NewTime(time.Now().Add(-time.Minute))}},
		}}},
	}
}

func TestWhyExplainsOOMAndSetsExitCode(t *testing.T) {
	cs := fake.NewSimpleClientset(oomPod())
	var out bytes.Buffer
	a := &app{out: &out, err: &bytes.Buffer{}, pal: term.Palette{}}
	r, err := a.buildWhy(context.Background(), cs, oomPod(), whyFlags{logLines: 5})
	if err != nil {
		t.Fatal(err)
	}
	a.printWhy(r)
	text := out.String()
	for _, want := range []string{"Pod a/oom", "CrashLoopBackOff", "Suspected causes", "memory limit", "OOMKilled"} {
		if !strings.Contains(text, want) {
			t.Errorf("output missing %q:\n%s", want, text)
		}
	}
	if r.Healthy {
		t.Error("OOM pod must not be healthy")
	}
}

func TestWhyHealthyPod(t *testing.T) {
	cs := fake.NewSimpleClientset(healthyPod("a", "fine"))
	a := &app{out: &bytes.Buffer{}, err: &bytes.Buffer{}, pal: term.Palette{}}
	a.opts.Namespace = "a"
	r, err := a.buildWhy(context.Background(), cs, healthyPod("a", "fine"), whyFlags{logLines: 5})
	if err != nil || !r.Healthy {
		t.Fatalf("healthy pod reported unhealthy: %+v %v", r, err)
	}
}
