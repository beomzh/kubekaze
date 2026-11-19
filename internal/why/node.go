package why

import (
	"fmt"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"

	"github.com/beomzh/kubekaze/internal/events"
)

// NodeEvidence is what DiagnoseNode looks at.
type NodeEvidence struct {
	Node   *corev1.Node
	Events []events.Row // events whose involved object is this node
	// BadPods describes pods on the node that are not healthy ("ns/name: status").
	BadPods []string
	Now     time.Time
}

// DiagnoseNode returns suspected causes for a node, most likely first. It
// returns nil when the node looks healthy.
func DiagnoseNode(ev NodeEvidence) []Cause {
	n := ev.Node
	if n == nil {
		return nil
	}
	var causes []Cause
	add := func(c Cause) { causes = append(causes, c) }

	for _, c := range n.Status.Conditions {
		switch c.Type {
		case corev1.NodeReady:
			stale := c.Status == corev1.ConditionTrue && !c.LastHeartbeatTime.IsZero() && ev.Now.Sub(c.LastHeartbeatTime.Time) > 2*time.Minute
			switch {
			case c.Status == corev1.ConditionUnknown || stale:
				hb := "no heartbeat recorded"
				if !c.LastHeartbeatTime.IsZero() {
					hb = "last heartbeat " + events.Age(ev.Now.Sub(c.LastHeartbeatTime.Time)) + " ago"
				}
				add(Cause{
					Title:    "The node stopped reporting to the control plane",
					Score:    95,
					Evidence: []string{"Ready is " + string(c.Status) + ", " + hb, nonEmpty(c.Reason + " " + c.Message)},
					Hint:     "The kubelet is down or the node is unreachable. Check the machine (power, cloud console, network, VPN) and, on the node, `systemctl status kubelet` and `journalctl -u kubelet`.",
				})
			case c.Status == corev1.ConditionFalse:
				add(Cause{
					Title:    "The kubelet reports the node as not ready",
					Score:    92,
					Evidence: []string{"Ready=False: " + nonEmpty(c.Reason+" "+c.Message)},
					Hint:     readyHint(c.Reason + " " + c.Message),
				})
			}
		case corev1.NodeDiskPressure:
			if c.Status == corev1.ConditionTrue {
				add(Cause{Title: "The node is low on disk space", Score: 88, Evidence: []string{condLine(c)},
					Hint: "Pods get evicted and images are garbage collected. On the node: `df -h`, `du -sh /var/lib/containerd /var/lib/kubelet /var/log`; remove unused images, rotate logs, or grow the disk."})
			}
		case corev1.NodeMemoryPressure:
			if c.Status == corev1.ConditionTrue {
				add(Cause{Title: "The node is low on memory", Score: 88, Evidence: []string{condLine(c)},
					Hint: "Pods are evicted by QoS class. Find the heavy pods (`kubectl top pods -A --sort-by=memory`) and set requests/limits, or add memory."})
			}
		case corev1.NodePIDPressure:
			if c.Status == corev1.ConditionTrue {
				add(Cause{Title: "The node is running out of process IDs", Score: 85, Evidence: []string{condLine(c)},
					Hint: "Some pod is forking too many processes. Set a pod PID limit (kubelet podPidsLimit) and find the offender."})
			}
		case corev1.NodeNetworkUnavailable:
			if c.Status == corev1.ConditionTrue {
				add(Cause{Title: "The node network is not set up", Score: 90, Evidence: []string{condLine(c)},
					Hint: "Usually the CNI plugin or cloud route controller. Check the CNI pods on this node (kubekaze status -n kube-system)."})
			}
		}
	}

	if n.Spec.Unschedulable {
		add(Cause{Title: "The node is cordoned (no new pods are scheduled)", Score: 45,
			Evidence: []string{"spec.unschedulable=true"}, Hint: "Intentional during maintenance. Undo with: kubectl uncordon " + n.Name})
	}
	for _, t := range n.Spec.Taints {
		if t.Effect == corev1.TaintEffectNoExecute || t.Effect == corev1.TaintEffectNoSchedule {
			// the unschedulable taint only mirrors "cordoned", which is reported on its own
			if strings.HasPrefix(t.Key, "node.kubernetes.io/") && t.Key != "node.kubernetes.io/unschedulable" {
				add(Cause{Title: "The control plane tainted the node (" + t.Key + ")", Score: 70,
					Evidence: []string{fmt.Sprintf("taint %s:%s", t.Key, t.Effect)},
					Hint:     "Automatic taints follow node conditions; they disappear when the underlying problem is fixed. NoExecute taints evict pods."})
			}
		}
	}

	for _, r := range ev.Events {
		if r.Type != "Warning" {
			continue
		}
		switch r.Reason {
		case "SystemOOM":
			add(Cause{Title: "The kernel killed processes on the node for lack of memory (system OOM)", Score: 86, Evidence: []string{evLine(r)},
				Hint: "Memory is overcommitted. Check pod memory limits vs requests and node allocatable."})
		case "ImageGCFailed", "FreeDiskSpaceFailed", "EvictionThresholdMet":
			add(Cause{Title: "The kubelet cannot free enough resources", Score: 84, Evidence: []string{evLine(r)}, Hint: "See the disk/memory causes above; reclaim space or capacity."})
		case "ContainerGCFailed", "KubeletSetupFailed", "FailedNodeAllocatableEnforcement", "InvalidDiskCapacity":
			add(Cause{Title: "The kubelet reports a node setup problem (" + r.Reason + ")", Score: 70, Evidence: []string{evLine(r)}, Hint: "Check the kubelet log on the node."})
		case "Rebooted":
			add(Cause{Title: "The node rebooted recently", Score: 60, Evidence: []string{evLine(r)}, Hint: "Pods were restarted by the reboot; find out why it rebooted (kernel panic, maintenance, autoscaler)."})
		case "NodeNotReady":
			add(Cause{Title: "The node changed to NotReady", Score: 65, Evidence: []string{evLine(r)}})
		}
	}

	if len(causes) > 0 && len(ev.BadPods) > 0 {
		shown := ev.BadPods
		more := ""
		if len(shown) > 5 {
			more = fmt.Sprintf(" (and %d more)", len(shown)-5)
			shown = shown[:5]
		}
		add(Cause{Title: fmt.Sprintf("%d pod(s) on this node are unhealthy", len(ev.BadPods)), Score: 40,
			Evidence: append(shown, strings.TrimSpace(more)), Hint: "Explain one of them: kubekaze why <pod> -n <namespace>"})
	}

	sort.SliceStable(causes, func(i, j int) bool { return causes[i].Score > causes[j].Score })
	for i := range causes {
		causes[i].Evidence = compact(causes[i].Evidence)
	}
	return dedupe(causes)
}

func nonEmpty(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "no reason given"
	}
	return s
}

func compact(in []string) []string {
	out := in[:0]
	for _, s := range in {
		if strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
	}
	return out
}

func condLine(c corev1.NodeCondition) string {
	return fmt.Sprintf("%s=%s: %s", c.Type, c.Status, nonEmpty(c.Reason+" "+c.Message))
}

func readyHint(s string) string {
	low := strings.ToLower(s)
	switch {
	case strings.Contains(low, "network") || strings.Contains(low, "cni"):
		return "The CNI plugin is not ready on this node. Check the CNI pods and /etc/cni/net.d on the node."
	case strings.Contains(low, "runtime") || strings.Contains(low, "containerd") || strings.Contains(low, "docker") || strings.Contains(low, "crio"):
		return "The container runtime is not healthy. On the node: `systemctl status containerd` (or crio) and its journal."
	case strings.Contains(low, "pleg"):
		return "The kubelet's pod lifecycle event generator is stuck, often because the runtime or the node is overloaded."
	case strings.Contains(low, "certificate") || strings.Contains(low, "unauthorized"):
		return "The kubelet cannot authenticate; look for expired or unapproved certificates (kubectl get csr)."
	}
	return "Check the kubelet log on the node: `journalctl -u kubelet`."
}
