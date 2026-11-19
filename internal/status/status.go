// Package status finds what is unhealthy in a set of Kubernetes objects.
// Healthy objects are never reported.
package status

import (
	"fmt"
	"sort"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"

	"github.com/beomzh/kubekaze/internal/podstatus"
)

// Severity of a finding.
type Severity string

const (
	Critical Severity = "critical"
	Warning  Severity = "warning"
)

// Finding is one unhealthy object.
type Finding struct {
	Severity  Severity `json:"severity"`
	Kind      string   `json:"kind"`
	Namespace string   `json:"namespace,omitempty"`
	Name      string   `json:"name"`
	Status    string   `json:"status"`
	Detail    string   `json:"detail,omitempty"`
}

// Ref returns kind/name in lower case, ready to pass to "why".
func (f Finding) Ref() string {
	return lower(f.Kind) + "/" + f.Name
}

// Input is everything Analyze looks at.
type Input struct {
	Pods         []corev1.Pod
	Deployments  []appsv1.Deployment
	StatefulSets []appsv1.StatefulSet
	DaemonSets   []appsv1.DaemonSet
	Jobs         []batchv1.Job
	Nodes        []corev1.Node
	PVCs         []corev1.PersistentVolumeClaim
	Now          time.Time
	// PendingGrace is how long Pending/ContainerCreating is tolerated.
	PendingGrace time.Duration
	// RestartThreshold flags running pods that restarted at least this often.
	RestartThreshold int
}

// Analyze returns the findings, most severe first.
func Analyze(in Input) []Finding {
	if in.PendingGrace == 0 {
		in.PendingGrace = 2 * time.Minute
	}
	if in.RestartThreshold == 0 {
		in.RestartThreshold = 5
	}
	var out []Finding
	out = append(out, nodes(in)...)
	out = append(out, workloads(in)...)
	out = append(out, pods(in)...)
	out = append(out, jobs(in)...)
	out = append(out, pvcs(in)...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Severity != out[j].Severity {
			return out[i].Severity == Critical
		}
		return false
	})
	return out
}

func lower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}

// heartbeatStale is how long a kubelet may be silent before the node is
// considered unreachable even if its Ready condition has not flipped yet.
const heartbeatStale = 2 * time.Minute

func nodes(in Input) []Finding {
	var out []Finding
	for _, n := range in.Nodes {
		ready, known := false, false
		var readyDetail string
		var problems []string
		for _, c := range n.Status.Conditions {
			switch c.Type {
			case corev1.NodeReady:
				known = c.Status != corev1.ConditionUnknown
				ready = c.Status == corev1.ConditionTrue
				readyDetail = condDetail(c)
				if ready && !c.LastHeartbeatTime.IsZero() && in.Now.Sub(c.LastHeartbeatTime.Time) > heartbeatStale {
					ready, known = false, false
					readyDetail = "no heartbeat for " + in.Now.Sub(c.LastHeartbeatTime.Time).Round(time.Second).String()
				}
			case corev1.NodeMemoryPressure, corev1.NodeDiskPressure, corev1.NodePIDPressure, corev1.NodeNetworkUnavailable:
				if c.Status == corev1.ConditionTrue {
					problems = append(problems, string(c.Type))
				}
			}
		}
		switch {
		case !ready && !known:
			out = append(out, Finding{Severity: Critical, Kind: "Node", Name: n.Name, Status: "Unknown", Detail: joinOr([]string{readyDetail}, "kubelet is not reporting (node down or unreachable?)")})
		case !ready:
			out = append(out, Finding{Severity: Critical, Kind: "Node", Name: n.Name, Status: "NotReady", Detail: joinOr([]string{readyDetail}, joinOr(problems, "kubelet reports not ready"))})
		case len(problems) > 0:
			out = append(out, Finding{Severity: Warning, Kind: "Node", Name: n.Name, Status: "Ready", Detail: joinOr(problems, "")})
		case n.Spec.Unschedulable:
			out = append(out, Finding{Severity: Warning, Kind: "Node", Name: n.Name, Status: "SchedulingDisabled", Detail: "cordoned"})
		}
	}
	return out
}

func condDetail(c corev1.NodeCondition) string {
	switch {
	case c.Reason != "" && c.Message != "":
		return c.Reason + ": " + c.Message
	case c.Reason != "":
		return c.Reason
	}
	return c.Message
}

func joinOr(items []string, fallback string) string {
	if len(items) == 0 {
		return fallback
	}
	s := items[0]
	for _, it := range items[1:] {
		s += ", " + it
	}
	return s
}

func workloads(in Input) []Finding {
	var out []Finding
	for _, d := range in.Deployments {
		want := int32(1)
		if d.Spec.Replicas != nil {
			want = *d.Spec.Replicas
		}
		if want == 0 {
			continue
		}
		if d.Status.AvailableReplicas < want {
			sev := Warning
			if d.Status.AvailableReplicas == 0 {
				sev = Critical
			}
			out = append(out, Finding{Severity: sev, Kind: "Deployment", Namespace: d.Namespace, Name: d.Name,
				Status: fmt.Sprintf("%d/%d available", d.Status.AvailableReplicas, want), Detail: deployDetail(d)})
		}
	}
	for _, s := range in.StatefulSets {
		want := int32(1)
		if s.Spec.Replicas != nil {
			want = *s.Spec.Replicas
		}
		if want > 0 && s.Status.ReadyReplicas < want {
			sev := Warning
			if s.Status.ReadyReplicas == 0 {
				sev = Critical
			}
			out = append(out, Finding{Severity: sev, Kind: "StatefulSet", Namespace: s.Namespace, Name: s.Name,
				Status: fmt.Sprintf("%d/%d ready", s.Status.ReadyReplicas, want)})
		}
	}
	for _, d := range in.DaemonSets {
		if d.Status.NumberUnavailable > 0 || d.Status.NumberReady < d.Status.DesiredNumberScheduled {
			out = append(out, Finding{Severity: Warning, Kind: "DaemonSet", Namespace: d.Namespace, Name: d.Name,
				Status: fmt.Sprintf("%d/%d ready", d.Status.NumberReady, d.Status.DesiredNumberScheduled)})
		}
	}
	return out
}

func deployDetail(d appsv1.Deployment) string {
	for _, c := range d.Status.Conditions {
		if c.Type == appsv1.DeploymentProgressing && c.Status == corev1.ConditionFalse {
			return c.Reason + ": " + c.Message
		}
	}
	for _, c := range d.Status.Conditions {
		if c.Type == appsv1.DeploymentAvailable && c.Status == corev1.ConditionFalse {
			return c.Reason
		}
	}
	return ""
}

func pods(in Input) []Finding {
	var out []Finding
	for i := range in.Pods {
		p := &in.Pods[i]
		st := podstatus.Of(p)
		age := in.Now.Sub(p.CreationTimestamp.Time)
		switch {
		case podstatus.Healthy(p):
			if r := podstatus.Restarts(p); r >= in.RestartThreshold {
				out = append(out, Finding{Severity: Warning, Kind: "Pod", Namespace: p.Namespace, Name: p.Name, Status: st, Detail: fmt.Sprintf("%d restarts", r)})
			}
		case (st == "Pending" || st == "ContainerCreating" || st == "PodInitializing" || len(st) > 5 && st[:5] == "Init:" && !isFailingInit(st)) && age < in.PendingGrace:
			// still starting up; give it a moment
		case st == "Terminating" && age < in.PendingGrace:
		default:
			sev := Warning
			switch st {
			case "CrashLoopBackOff", "OOMKilled", "ImagePullBackOff", "ErrImagePull", "CreateContainerConfigError", "Error", "Evicted", "Failed", "Unknown":
				sev = Critical
			}
			out = append(out, Finding{Severity: sev, Kind: "Pod", Namespace: p.Namespace, Name: p.Name, Status: st, Detail: podDetail(p)})
		}
	}
	return out
}

func isFailingInit(st string) bool {
	// Init:0/2 style is progress; anything else after "Init:" is a failure.
	return len(st) > 5 && !(st[5] >= '0' && st[5] <= '9')
}

func podDetail(p *corev1.Pod) string {
	if r := podstatus.Restarts(p); r > 0 {
		return fmt.Sprintf("%d restarts", r)
	}
	for _, c := range p.Status.Conditions {
		if c.Type == corev1.PodScheduled && c.Status == corev1.ConditionFalse {
			return c.Reason + ": " + c.Message
		}
	}
	return ""
}

func jobs(in Input) []Finding {
	var out []Finding
	for _, j := range in.Jobs {
		for _, c := range j.Status.Conditions {
			if c.Type == batchv1.JobFailed && c.Status == corev1.ConditionTrue {
				out = append(out, Finding{Severity: Warning, Kind: "Job", Namespace: j.Namespace, Name: j.Name, Status: "Failed", Detail: c.Reason + ": " + c.Message})
			}
		}
	}
	return out
}

func pvcs(in Input) []Finding {
	var out []Finding
	for _, c := range in.PVCs {
		switch c.Status.Phase {
		case corev1.ClaimPending:
			if in.Now.Sub(c.CreationTimestamp.Time) > in.PendingGrace {
				out = append(out, Finding{Severity: Warning, Kind: "PersistentVolumeClaim", Namespace: c.Namespace, Name: c.Name, Status: "Pending"})
			}
		case corev1.ClaimLost:
			out = append(out, Finding{Severity: Critical, Kind: "PersistentVolumeClaim", Namespace: c.Namespace, Name: c.Name, Status: "Lost"})
		}
	}
	return out
}
