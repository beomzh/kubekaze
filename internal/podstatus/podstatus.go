// Package podstatus computes the human readable pod status that kubectl
// shows in the STATUS column (CrashLoopBackOff, Evicted, Completed, ...).
package podstatus

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
)

// Of returns the display status of a pod.
func Of(pod *corev1.Pod) string {
	reason := string(pod.Status.Phase)
	if pod.Status.Reason != "" {
		reason = pod.Status.Reason // e.g. Evicted, NodeLost, Shutdown
	}

	initializing := false
	for i, c := range pod.Status.InitContainerStatuses {
		switch {
		case c.State.Terminated != nil && c.State.Terminated.ExitCode == 0:
			continue
		case c.State.Terminated != nil:
			if c.State.Terminated.Reason != "" {
				reason = "Init:" + c.State.Terminated.Reason
			} else if c.State.Terminated.Signal != 0 {
				reason = fmt.Sprintf("Init:Signal:%d", c.State.Terminated.Signal)
			} else {
				reason = fmt.Sprintf("Init:ExitCode:%d", c.State.Terminated.ExitCode)
			}
			initializing = true
		case c.State.Waiting != nil && c.State.Waiting.Reason != "" && c.State.Waiting.Reason != "PodInitializing":
			reason = "Init:" + c.State.Waiting.Reason
			initializing = true
		default:
			reason = fmt.Sprintf("Init:%d/%d", i, len(pod.Spec.InitContainers))
			initializing = true
		}
		break
	}

	if !initializing {
		hasRunning := false
		for i := len(pod.Status.ContainerStatuses) - 1; i >= 0; i-- {
			c := pod.Status.ContainerStatuses[i]
			switch {
			case c.State.Waiting != nil && c.State.Waiting.Reason != "":
				reason = c.State.Waiting.Reason
			case c.State.Terminated != nil && c.State.Terminated.Reason != "":
				reason = c.State.Terminated.Reason
			case c.State.Terminated != nil && c.State.Terminated.Signal != 0:
				reason = fmt.Sprintf("Signal:%d", c.State.Terminated.Signal)
			case c.State.Terminated != nil:
				reason = fmt.Sprintf("ExitCode:%d", c.State.Terminated.ExitCode)
			case c.Ready && c.State.Running != nil:
				hasRunning = true
			}
		}
		if reason == "Completed" && hasRunning {
			reason = "Running"
		}
	}

	if pod.DeletionTimestamp != nil {
		if pod.Status.Reason == "NodeLost" {
			return "Unknown"
		}
		return "Terminating"
	}
	if reason == "Succeeded" {
		return "Completed"
	}
	if reason == "Failed" {
		return "Failed"
	}
	return reason
}

// Restarts returns the total container restart count.
func Restarts(pod *corev1.Pod) int {
	n := 0
	for _, c := range pod.Status.ContainerStatuses {
		n += int(c.RestartCount)
	}
	return n
}

// Healthy reports whether a pod looks fine: Running with all containers
// ready, or Completed (a finished Job pod).
func Healthy(pod *corev1.Pod) bool {
	switch Of(pod) {
	case "Completed":
		return true
	case "Running":
		for _, c := range pod.Status.ContainerStatuses {
			if !c.Ready {
				return false
			}
		}
		return true
	}
	return false
}
