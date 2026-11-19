// Package why turns the evidence about a pod into ranked, explainable
// suspected causes using deterministic rules (no AI).
package why

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"

	"github.com/beomzh/kubekaze/internal/events"
	"github.com/beomzh/kubekaze/internal/podstatus"
)

// Cause is one suspected reason with the evidence that supports it.
type Cause struct {
	Title    string   `json:"title"`
	Score    int      `json:"score"`
	Evidence []string `json:"evidence"`
	Hint     string   `json:"hint,omitempty"`
}

// Evidence is what Diagnose looks at.
type Evidence struct {
	Pod    *corev1.Pod
	Events []events.Row // events of this pod
	// LogTail is the last log lines of the failing container (already redacted).
	LogTail []string
	// RolloutAge, when non-zero, is how long ago the owning workload started
	// its newest rollout.
	RolloutAge time.Duration
	Now        time.Time
}

// Diagnose returns causes ordered by score (highest first). It returns nil
// when the pod looks healthy.
func Diagnose(ev Evidence) []Cause {
	p := ev.Pod
	if p == nil || podstatus.Healthy(p) && podstatus.Restarts(p) == 0 {
		return nil
	}
	var causes []Cause
	add := func(c Cause) { causes = append(causes, c) }
	st := podstatus.Of(p)

	// Terminated reasons and exit codes, current and last.
	for _, cs := range p.Status.ContainerStatuses {
		for _, t := range []*corev1.ContainerStateTerminated{cs.State.Terminated, cs.LastTerminationState.Terminated} {
			if t == nil {
				continue
			}
			switch {
			case t.Reason == "OOMKilled":
				mem := memoryLimit(p, cs.Name)
				add(Cause{
					Title:    fmt.Sprintf("Container %q was killed for exceeding its memory limit", cs.Name),
					Score:    95,
					Evidence: []string{fmt.Sprintf("last termination: OOMKilled (exit code %d) at %s", t.ExitCode, t.FinishedAt.Format(time.RFC3339)), "memory limit: " + mem},
					Hint:     "Raise the memory limit, or look for a memory leak. Compare with real usage: kubectl top pod.",
				})
			case t.ExitCode == 137 && t.Reason != "OOMKilled":
				add(Cause{
					Title:    fmt.Sprintf("Container %q was killed with SIGKILL (exit 137)", cs.Name),
					Score:    60,
					Evidence: []string{"exit code 137 without an OOMKilled reason"},
					Hint:     "Often a failing liveness probe, node memory pressure, or a manual kill. Check the probe events below.",
				})
			case t.ExitCode != 0 && t.Reason != "Completed":
				add(Cause{
					Title:    fmt.Sprintf("Container %q exits with an error (exit code %d)", cs.Name, t.ExitCode),
					Score:    80,
					Evidence: append([]string{fmt.Sprintf("termination reason: %s, exit code %d", orDash(t.Reason), t.ExitCode)}, tail(ev.LogTail, 3)...),
					Hint:     "The application fails on its own. Read the log lines above (kubekaze logs <pod> includes the previous run).",
				})
			}
		}
		if w := cs.State.Waiting; w != nil {
			switch w.Reason {
			case "ImagePullBackOff", "ErrImagePull", "InvalidImageName":
				add(imagePull(cs.Image, w, ev.Events))
			case "CreateContainerConfigError", "CreateContainerError":
				add(Cause{
					Title:    "The container could not be created, usually a missing ConfigMap/Secret or key",
					Score:    90,
					Evidence: []string{w.Reason + ": " + w.Message},
					Hint:     "Check that every referenced ConfigMap/Secret and key exists in this namespace.",
				})
			case "CrashLoopBackOff":
				if !has(causes, "exits with an error", "OOMKilled", "SIGKILL") {
					add(Cause{Title: "The container keeps crashing (CrashLoopBackOff)", Score: 70,
						Evidence: append([]string{"waiting reason: CrashLoopBackOff"}, tail(ev.LogTail, 3)...),
						Hint:     "See the previous container's logs for the crash reason."})
				}
			}
		}
	}

	// Init containers
	for _, ic := range p.Status.InitContainerStatuses {
		if w := ic.State.Waiting; w != nil && w.Reason != "PodInitializing" && w.Reason != "" {
			add(Cause{Title: fmt.Sprintf("Init container %q is failing (%s)", ic.Name, w.Reason), Score: 85,
				Evidence: []string{w.Message}, Hint: "The pod will not start until its init containers succeed."})
		}
	}

	// Scheduling
	for _, c := range p.Status.Conditions {
		if c.Type == corev1.PodScheduled && c.Status == corev1.ConditionFalse {
			add(scheduling(c))
		}
	}

	// Events
	for _, r := range ev.Events {
		if r.Type != "Warning" {
			continue
		}
		switch r.Reason {
		case "Unhealthy":
			kind := "probe"
			if strings.Contains(r.Message, "Liveness") {
				kind = "liveness probe"
			} else if strings.Contains(r.Message, "Readiness") {
				kind = "readiness probe"
			}
			add(Cause{Title: "A " + kind + " is failing", Score: 75, Evidence: []string{evLine(r)},
				Hint: "Check the probe path/port/timeouts and whether the app is slow to start (consider a startupProbe)."})
		case "FailedMount", "FailedAttachVolume":
			add(Cause{Title: "A volume could not be mounted or attached", Score: 85, Evidence: []string{evLine(r)},
				Hint: "Check the PVC/PV status, the storage class, and that referenced Secrets/ConfigMaps exist."})
		case "Evicted", "Preempting":
			add(Cause{Title: "The pod was evicted or preempted", Score: 80, Evidence: []string{evLine(r)}, Hint: "Look at node pressure (kubectl describe node) and pod priorities."})
		case "FailedCreate", "FailedCreatePodSandBox":
			add(Cause{Title: "The pod sandbox/network could not be created", Score: 80, Evidence: []string{evLine(r)}, Hint: "Often a CNI or runtime problem on the node."})
		}
	}
	if st == "Evicted" {
		add(Cause{Title: "The pod was evicted", Score: 90, Evidence: []string{"pod reason: " + p.Status.Reason + ": " + p.Status.Message},
			Hint: "Evicted pods are not restarted. Fix node pressure; delete leftovers with: kubekaze rm pods --status Evicted"})
	}
	if p.DeletionTimestamp != nil && ev.Now.Sub(p.DeletionTimestamp.Time) > time.Minute {
		add(Cause{Title: "The pod is stuck terminating", Score: 85,
			Evidence: []string{fmt.Sprintf("deletion requested %s ago", events.Age(ev.Now.Sub(p.DeletionTimestamp.Time))), fmt.Sprintf("finalizers: %v", p.Finalizers)},
			Hint: "Look for finalizers, an unresponsive node, or a process that ignores SIGTERM."})
	}

	// Context: a recent rollout often explains the change.
	if ev.RolloutAge > 0 && ev.RolloutAge < 30*time.Minute && len(causes) > 0 {
		add(Cause{Title: "A rollout started recently", Score: 40,
			Evidence: []string{fmt.Sprintf("newest ReplicaSet created %s ago", events.Age(ev.RolloutAge))},
			Hint:     "The problem may come from the new version. Compare with the previous revision: kubectl rollout history."})
	}

	sort.SliceStable(causes, func(i, j int) bool { return causes[i].Score > causes[j].Score })
	return dedupe(causes)
}

func dedupe(cs []Cause) []Cause {
	seen := map[string]bool{}
	out := cs[:0]
	for _, c := range cs {
		if seen[c.Title] {
			continue
		}
		seen[c.Title] = true
		out = append(out, c)
	}
	return out
}

func has(cs []Cause, subs ...string) bool {
	for _, c := range cs {
		for _, s := range subs {
			if strings.Contains(c.Title, s) {
				return true
			}
		}
	}
	return false
}

func imagePull(image string, w *corev1.ContainerStateWaiting, evs []events.Row) Cause {
	msg := w.Message
	for _, r := range evs {
		if r.Reason == "Failed" && strings.Contains(r.Message, "image") {
			msg = r.Message
		}
	}
	hint := "Check the image name and tag, and that the registry is reachable."
	low := strings.ToLower(msg)
	switch {
	case strings.Contains(low, "not found") || strings.Contains(low, "manifest unknown"):
		hint = "The image or tag does not exist. Check for a typo in the name/tag."
	case strings.Contains(low, "unauthorized") || strings.Contains(low, "denied") || strings.Contains(low, "authentication"):
		hint = "The registry rejected the credentials. Check imagePullSecrets in the pod/ServiceAccount."
	case strings.Contains(low, "timeout") || strings.Contains(low, "no such host") || strings.Contains(low, "connection refused"):
		hint = "The node cannot reach the registry. Check DNS, proxy and network policy."
	case strings.Contains(low, "toomanyrequests") || strings.Contains(low, "rate limit"):
		hint = "The registry is rate limiting pulls. Use a mirror or authenticate."
	}
	return Cause{Title: fmt.Sprintf("The image %q cannot be pulled", image), Score: 95, Evidence: []string{w.Reason + ": " + oneLine(msg)}, Hint: hint}
}

var insufficient = regexp.MustCompile(`Insufficient ([a-z0-9./-]+)`)

func scheduling(c corev1.PodCondition) Cause {
	msg := c.Message
	cause := Cause{Title: "The pod cannot be scheduled onto any node", Score: 90, Evidence: []string{c.Reason + ": " + oneLine(msg)}}
	switch {
	case insufficient.MatchString(msg):
		res := insufficient.FindStringSubmatch(msg)[1]
		cause.Title = "No node has enough " + res + " for this pod"
		cause.Hint = "Lower the pod's " + res + " request, add nodes, or free capacity."
	case strings.Contains(msg, "untolerated taint"):
		cause.Title = "Every node has a taint the pod does not tolerate"
		cause.Hint = "Add a matching toleration, or use nodes without that taint."
	case strings.Contains(msg, "node affinity") || strings.Contains(msg, "node selector"):
		cause.Title = "No node matches the pod's nodeSelector / affinity"
		cause.Hint = "Check nodeSelector and affinity against the node labels."
	case strings.Contains(msg, "unbound") || strings.Contains(msg, "PersistentVolumeClaim"):
		cause.Title = "The pod waits for a PersistentVolumeClaim that is not bound"
		cause.Hint = "Check the PVC and its StorageClass (kubectl get pvc)."
	}
	return cause
}

func memoryLimit(p *corev1.Pod, container string) string {
	for _, c := range p.Spec.Containers {
		if c.Name == container {
			if q, ok := c.Resources.Limits[corev1.ResourceMemory]; ok {
				return q.String()
			}
			return "none set"
		}
	}
	return "unknown"
}

func evLine(r events.Row) string {
	s := fmt.Sprintf("%s: %s", r.Reason, oneLine(r.Message))
	if r.Count > 1 {
		s += fmt.Sprintf(" (x%d)", r.Count)
	}
	return s
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func tail(lines []string, n int) []string {
	if len(lines) <= n {
		return lines
	}
	return lines[len(lines)-n:]
}
