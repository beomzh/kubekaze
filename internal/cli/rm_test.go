package cli

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/beomzh/kubekaze/internal/term"
)

func crashPod(ns, name string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name, UID: types.UID("uid-" + name), CreationTimestamp: metav1.NewTime(time.Now().Add(-3 * time.Hour))},
		Status: corev1.PodStatus{Phase: corev1.PodRunning, ContainerStatuses: []corev1.ContainerStatus{
			{State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}}}}},
	}
}

func healthyPod(ns, name string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name, UID: types.UID("uid-" + name), CreationTimestamp: metav1.NewTime(time.Now().Add(-3 * time.Hour))},
		Status: corev1.PodStatus{Phase: corev1.PodRunning, ContainerStatuses: []corev1.ContainerStatus{
			{Ready: true, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}}}},
	}
}

func newRmApp(input string, interactive bool) (*app, *bytes.Buffer, *bytes.Buffer) {
	var out, errb bytes.Buffer
	return &app{in: strings.NewReader(input), out: &out, err: &errb, pal: term.Palette{}, interactive: interactive}, &out, &errb
}

func podCount(t *testing.T, cs *fake.Clientset, ns string) int {
	t.Helper()
	l, err := cs.CoreV1().Pods(ns).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return len(l.Items)
}

func TestRmDryRunDeletesNothing(t *testing.T) {
	cs := fake.NewSimpleClientset(crashPod("a", "p1"), healthyPod("a", "p2"))
	a, _, errb := newRmApp("", false)
	err := a.runRm(context.Background(), cs, "dev", []string{"a"}, nil, rmFlags{statuses: []string{"CrashLoopBackOff"}, dryRun: true, max: 50, grace: -1})
	if err != nil {
		t.Fatal(err)
	}
	if podCount(t, cs, "a") != 2 {
		t.Error("dry run deleted pods")
	}
	if !strings.Contains(errb.String(), "p1") || strings.Contains(errb.String(), "p2") {
		t.Errorf("preview should list only p1: %s", errb.String())
	}
}

func TestRmYesDeletesOnlyMatches(t *testing.T) {
	cs := fake.NewSimpleClientset(crashPod("a", "p1"), healthyPod("a", "p2"))
	a, out, _ := newRmApp("", false)
	err := a.runRm(context.Background(), cs, "dev", []string{"a"}, nil, rmFlags{statuses: []string{"CrashLoopBackOff"}, yes: true, max: 50, grace: -1})
	if err != nil {
		t.Fatal(err)
	}
	if podCount(t, cs, "a") != 1 {
		t.Error("expected exactly one pod left")
	}
	if !strings.Contains(out.String(), "deleted a/p1") || !strings.Contains(out.String(), "1 deleted, 0 failed") {
		t.Errorf("output: %s", out.String())
	}
}

func TestRmRefusesWithoutConfirmationOnNonTerminal(t *testing.T) {
	cs := fake.NewSimpleClientset(crashPod("a", "p1"))
	a, _, _ := newRmApp("y\n", false)
	err := a.runRm(context.Background(), cs, "dev", []string{"a"}, nil, rmFlags{statuses: []string{"CrashLoopBackOff"}, max: 50, grace: -1})
	if err == nil || podCount(t, cs, "a") != 1 {
		t.Errorf("should refuse and delete nothing, err=%v", err)
	}
}

func TestRmNoSelectionOnNonTerminalIsError(t *testing.T) {
	cs := fake.NewSimpleClientset(crashPod("a", "p1"))
	a, _, _ := newRmApp("", false)
	if err := a.runRm(context.Background(), cs, "dev", []string{"a"}, nil, rmFlags{yes: true, max: 50, grace: -1}); err == nil {
		t.Error("expected error when nothing selects pods")
	}
	if podCount(t, cs, "a") != 1 {
		t.Error("pods were deleted without a selection")
	}
}

func TestRmProductionNeedsConfirmContext(t *testing.T) {
	cs := fake.NewSimpleClientset(crashPod("a", "p1"))
	a, _, _ := newRmApp("", false)
	f := rmFlags{statuses: []string{"CrashLoopBackOff"}, yes: true, max: 50, grace: -1}
	if err := a.runRm(context.Background(), cs, "prod-eks", []string{"a"}, nil, f); err == nil {
		t.Fatal("expected refusal on production context without --confirm-context")
	}
	if podCount(t, cs, "a") != 1 {
		t.Fatal("pod deleted despite refusal")
	}
	f.confirmCtx = "prod-eks"
	if err := a.runRm(context.Background(), cs, "prod-eks", []string{"a"}, nil, f); err != nil {
		t.Fatal(err)
	}
	if podCount(t, cs, "a") != 0 {
		t.Error("pod should be deleted with matching --confirm-context")
	}
}

func TestRmMaxCap(t *testing.T) {
	var objs []*corev1.Pod
	for i := 0; i < 5; i++ {
		objs = append(objs, crashPod("a", fmt.Sprintf("p%d", i)))
	}
	cs := fake.NewSimpleClientset()
	for _, o := range objs {
		if _, err := cs.CoreV1().Pods("a").Create(context.Background(), o, metav1.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	a, _, _ := newRmApp("", false)
	err := a.runRm(context.Background(), cs, "dev", []string{"a"}, nil, rmFlags{statuses: []string{"CrashLoopBackOff"}, yes: true, max: 3, grace: -1})
	if err == nil || podCount(t, cs, "a") != 5 {
		t.Errorf("cap should block deletion, err=%v", err)
	}
}

func TestRmInteractiveConfirmation(t *testing.T) {
	cs := fake.NewSimpleClientset(crashPod("a", "p1"))
	a, _, _ := newRmApp("n\n", true)
	err := a.runRm(context.Background(), cs, "dev", []string{"a"}, nil, rmFlags{statuses: []string{"CrashLoopBackOff"}, max: 50, grace: -1})
	if err == nil || podCount(t, cs, "a") != 1 {
		t.Errorf("answer n must abort, err=%v", err)
	}
	a, _, _ = newRmApp("y\n", true)
	if err := a.runRm(context.Background(), cs, "dev", []string{"a"}, nil, rmFlags{statuses: []string{"CrashLoopBackOff"}, max: 50, grace: -1}); err != nil {
		t.Fatal(err)
	}
	if podCount(t, cs, "a") != 0 {
		t.Error("answer y should delete")
	}
}

func TestConfirmation(t *testing.T) {
	if _, e := confirmation(2, false, "dev"); e != "y" {
		t.Errorf("small: expect y, got %q", e)
	}
	if _, e := confirmation(12, false, "dev"); e != "12" {
		t.Errorf("many: expect count, got %q", e)
	}
	if _, e := confirmation(1, true, "prod"); e != "prod" {
		t.Errorf("prod: expect ctx name, got %q", e)
	}
}
