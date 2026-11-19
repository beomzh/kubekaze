package status

import (
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var now = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

func meta(ns, name string, ageMin int) metav1.ObjectMeta {
	return metav1.ObjectMeta{Namespace: ns, Name: name, CreationTimestamp: metav1.NewTime(now.Add(-time.Duration(ageMin) * time.Minute))}
}

func runningPod(name string, restarts int32) corev1.Pod {
	return corev1.Pod{ObjectMeta: meta("a", name, 60), Status: corev1.PodStatus{Phase: corev1.PodRunning,
		ContainerStatuses: []corev1.ContainerStatus{{Ready: true, RestartCount: restarts, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}}}}}
}

func find(fs []Finding, kind, name string) *Finding {
	for i := range fs {
		if fs[i].Kind == kind && fs[i].Name == name {
			return &fs[i]
		}
	}
	return nil
}

func TestHealthyClusterHasNoFindings(t *testing.T) {
	one := int32(1)
	in := Input{Now: now,
		Pods:        []corev1.Pod{runningPod("ok", 0)},
		Deployments: []appsv1.Deployment{{ObjectMeta: meta("a", "d", 60), Spec: appsv1.DeploymentSpec{Replicas: &one}, Status: appsv1.DeploymentStatus{AvailableReplicas: 1}}},
		Nodes:       []corev1.Node{{ObjectMeta: meta("", "n1", 600), Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}}}}},
	}
	if got := Analyze(in); len(got) != 0 {
		t.Errorf("expected no findings, got %+v", got)
	}
}

func TestPodFindings(t *testing.T) {
	crash := corev1.Pod{ObjectMeta: meta("a", "crash", 60), Status: corev1.PodStatus{Phase: corev1.PodRunning,
		ContainerStatuses: []corev1.ContainerStatus{{RestartCount: 12, State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}}}}}}
	newPending := corev1.Pod{ObjectMeta: meta("a", "fresh", 1), Status: corev1.PodStatus{Phase: corev1.PodPending}}
	oldPending := corev1.Pod{ObjectMeta: meta("a", "stuck", 30), Status: corev1.PodStatus{Phase: corev1.PodPending,
		Conditions: []corev1.PodCondition{{Type: corev1.PodScheduled, Status: corev1.ConditionFalse, Reason: "Unschedulable", Message: "0/3 nodes available"}}}}
	flappy := runningPod("flappy", 9)
	completed := corev1.Pod{ObjectMeta: meta("a", "job-pod", 60), Status: corev1.PodStatus{Phase: corev1.PodSucceeded}}
	got := Analyze(Input{Now: now, Pods: []corev1.Pod{crash, newPending, oldPending, flappy, completed}})

	if f := find(got, "Pod", "crash"); f == nil || f.Severity != Critical || f.Status != "CrashLoopBackOff" {
		t.Errorf("crash finding wrong: %+v", f)
	}
	if find(got, "Pod", "fresh") != nil {
		t.Error("freshly created pending pod should be tolerated")
	}
	if f := find(got, "Pod", "stuck"); f == nil || f.Detail == "" {
		t.Errorf("stuck pending pod should be reported with detail: %+v", f)
	}
	if f := find(got, "Pod", "flappy"); f == nil || f.Severity != Warning {
		t.Errorf("flappy pod should be a warning: %+v", f)
	}
	if find(got, "Pod", "job-pod") != nil {
		t.Error("completed pods are healthy")
	}
	if got[0].Severity != Critical {
		t.Error("critical findings must come first")
	}
}

func TestWorkloadNodeJobPVC(t *testing.T) {
	three := int32(3)
	zero := int32(0)
	in := Input{Now: now,
		Deployments: []appsv1.Deployment{
			{ObjectMeta: meta("a", "down", 60), Spec: appsv1.DeploymentSpec{Replicas: &three}, Status: appsv1.DeploymentStatus{AvailableReplicas: 0}},
			{ObjectMeta: meta("a", "partial", 60), Spec: appsv1.DeploymentSpec{Replicas: &three}, Status: appsv1.DeploymentStatus{AvailableReplicas: 2}},
			{ObjectMeta: meta("a", "scaled0", 60), Spec: appsv1.DeploymentSpec{Replicas: &zero}},
		},
		Nodes: []corev1.Node{
			{ObjectMeta: meta("", "bad", 600), Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionFalse}}}},
			{ObjectMeta: meta("", "pressure", 600), Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{
				{Type: corev1.NodeReady, Status: corev1.ConditionTrue}, {Type: corev1.NodeDiskPressure, Status: corev1.ConditionTrue}}}},
		},
		Jobs: []batchv1.Job{{ObjectMeta: meta("a", "bad-job", 60), Status: batchv1.JobStatus{Conditions: []batchv1.JobCondition{{Type: batchv1.JobFailed, Status: corev1.ConditionTrue, Reason: "BackoffLimitExceeded"}}}}},
		PVCs: []corev1.PersistentVolumeClaim{{ObjectMeta: meta("a", "data", 30), Status: corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimPending}}},
	}
	got := Analyze(in)
	if f := find(got, "Deployment", "down"); f == nil || f.Severity != Critical {
		t.Errorf("down: %+v", f)
	}
	if f := find(got, "Deployment", "partial"); f == nil || f.Severity != Warning {
		t.Errorf("partial: %+v", f)
	}
	if find(got, "Deployment", "scaled0") != nil {
		t.Error("deployment scaled to 0 is intentional")
	}
	if f := find(got, "Node", "bad"); f == nil || f.Severity != Critical {
		t.Errorf("node bad: %+v", f)
	}
	if f := find(got, "Node", "pressure"); f == nil || f.Detail != "DiskPressure" {
		t.Errorf("node pressure: %+v", f)
	}
	if find(got, "Job", "bad-job") == nil || find(got, "PersistentVolumeClaim", "data") == nil {
		t.Error("job/pvc findings missing")
	}
}

func TestRef(t *testing.T) {
	if got := (Finding{Kind: "Deployment", Name: "api"}).Ref(); got != "deployment/api" {
		t.Errorf("Ref() = %s", got)
	}
}

func TestNodeDetails(t *testing.T) {
	hb := func(ago time.Duration) metav1.Time { return metav1.NewTime(now.Add(-ago)) }
	in := Input{Now: now, Nodes: []corev1.Node{
		{ObjectMeta: meta("", "notready", 600), Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{
			{Type: corev1.NodeReady, Status: corev1.ConditionFalse, Reason: "KubeletNotReady", Message: "container runtime network not ready", LastHeartbeatTime: hb(5 * time.Second)}}}},
		{ObjectMeta: meta("", "gone", 600), Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{
			{Type: corev1.NodeReady, Status: corev1.ConditionUnknown, Reason: "NodeStatusUnknown", Message: "Kubelet stopped posting node status.", LastHeartbeatTime: hb(10 * time.Minute)}}}},
		{ObjectMeta: meta("", "silent", 600), Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{
			{Type: corev1.NodeReady, Status: corev1.ConditionTrue, LastHeartbeatTime: hb(6 * time.Minute)}}}},
		{ObjectMeta: meta("", "cordoned", 600), Spec: corev1.NodeSpec{Unschedulable: true}, Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{
			{Type: corev1.NodeReady, Status: corev1.ConditionTrue, LastHeartbeatTime: hb(5 * time.Second)}}}},
		{ObjectMeta: meta("", "fine", 600), Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{
			{Type: corev1.NodeReady, Status: corev1.ConditionTrue, LastHeartbeatTime: hb(5 * time.Second)}}}},
	}}
	got := Analyze(in)
	if f := find(got, "Node", "notready"); f == nil || f.Status != "NotReady" || f.Detail != "KubeletNotReady: container runtime network not ready" {
		t.Errorf("notready: %+v", f)
	}
	if f := find(got, "Node", "gone"); f == nil || f.Status != "Unknown" || f.Severity != Critical {
		t.Errorf("gone: %+v", f)
	}
	if f := find(got, "Node", "silent"); f == nil || f.Status != "Unknown" || f.Detail == "" {
		t.Errorf("a stale heartbeat must be reported even if Ready=True: %+v", f)
	}
	if f := find(got, "Node", "cordoned"); f == nil || f.Status != "SchedulingDisabled" {
		t.Errorf("cordoned: %+v", f)
	}
	if find(got, "Node", "fine") != nil {
		t.Error("healthy node must not be reported")
	}
}
