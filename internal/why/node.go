package why

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"

	"github.com/beomzh/kubekaze/internal/events"
	"github.com/beomzh/kubekaze/internal/status"
)

// NodeEvidence is what DiagnoseNode looks at.
type NodeEvidence struct {
	Node   *corev1.Node
	Events []events.Row // events whose involved object is this node
	// BadPods are the pods on the node that are not healthy (and not just starting).
	BadPods []NodePod
	// Restarts are the containers on the node that were restarted at least once,
	// with when their last run ended.
	Restarts []PodRestart
	Now      time.Time
}

// PodRestart is a container that has been restarted: how often and when its
// last run ended.
type PodRestart struct {
	Namespace, Pod, Container string
	Count                     int32
	Last                      time.Time
	ExitCode                  int32
	Reason                    string // termination reason of the last run, e.g. Error, OOMKilled
}

func (r PodRestart) String() string {
	return fmt.Sprintf("%s/%s (%s)", r.Namespace, r.Pod, r.Container)
}

// NodePod is an unhealthy pod running on the node.
type NodePod struct {
	Namespace, Name, Status string
	// Message is the runtime's explanation of why a container is not running.
	Message string
	Age     time.Duration
}

func (p NodePod) String() string { return fmt.Sprintf("%s/%s: %s", p.Namespace, p.Name, p.Status) }

// eventWindow is how recent a node event must be to count as a cause. Some
// distributions (OpenShift) keep events for days; a warning from two weeks ago
// is history, not a diagnosis of today's problem.
const eventWindow = 2 * time.Hour

const (
	// rebootWindow is how long a Rebooted event stays interesting: a reboot is a
	// fact about the node's past, not a state that clears itself.
	rebootWindow = 7 * 24 * time.Hour
	// restartWindow is how far back container restarts are looked at.
	restartWindow = 7 * 24 * time.Hour
	// burstSpan and burstPods define "several pods restarted at the same moment".
	burstSpan = 10 * time.Minute
	burstPods = 4
	// pastAfter is how long without a repeat turns a one-off node event (a
	// restart burst, a reboot) into history instead of a current problem.
	pastAfter = 24 * time.Hour
	// manyRestarts is the restart count from which a container is called a frequent restarter.
	manyRestarts = 10
)

// cniPod matches the node-level network agents of common CNI plugins.
var cniPod = regexp.MustCompile(`^(ovnkube-node|ovs-node|ovs-.*|calico-node|cilium(-[a-z0-9]+)?|aws-node|azure-cni.*|azure-ip-masq-agent|flannel.*|kube-flannel.*|kube-router.*|antrea-agent|weave-net.*|multus.*|canal.*|sdn(-[a-z0-9]+)?)(-[a-z0-9]{5,})?$`)

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
			stale := c.Status == corev1.ConditionTrue && !c.LastHeartbeatTime.IsZero() && ev.Now.Sub(c.LastHeartbeatTime.Time) > status.HeartbeatStale
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
		if !r.Last.IsZero() && ev.Now.Sub(r.Last) > eventWindow && !(r.Reason == "Rebooted" && ev.Now.Sub(r.Last) <= rebootWindow) {
			continue // history, not today's problem
		}
		switch r.Reason {
		case "SystemOOM":
			add(Cause{Title: "The kernel killed processes on the node for lack of memory (system OOM)", Score: 86, Evidence: []string{evLine(r)},
				Hint: "Memory is overcommitted. Check pod memory limits vs requests and node allocatable."})
		case "ImageGCFailed", "FreeDiskSpaceFailed", "EvictionThresholdMet":
			low := strings.ToLower(r.Message)
			if strings.Contains(low, "not found") || strings.Contains(low, "does not exist") || strings.Contains(low, "notfound") {
				// the runtime lost track of a container that was already gone: a harmless race
				add(Cause{Title: "The kubelet's image cleanup tripped over a container that no longer exists", Score: 25, Evidence: []string{evLine(r)},
					Hint: "Usually transient noise between the kubelet and the container runtime. Only worth acting on if it repeats together with disk pressure."})
				continue
			}
			add(Cause{Title: "The kubelet cannot free enough resources", Score: 84, Evidence: []string{evLine(r)}, Hint: "See the disk/memory causes above; reclaim space or capacity."})
		case "ContainerGCFailed", "KubeletSetupFailed", "FailedNodeAllocatableEnforcement", "InvalidDiskCapacity":
			add(Cause{Title: "The kubelet reports a node setup problem (" + r.Reason + ")", Score: 70, Evidence: []string{evLine(r)}, Hint: "Check the kubelet log on the node."})
		case "Rebooted":
			score := 60
			if !r.Last.IsZero() && ev.Now.Sub(r.Last) > eventWindow {
				score = 50 // already a while ago, the node has been up since
			}
			add(Cause{Title: "The node rebooted recently", Score: score, Past: !r.Last.IsZero() && ev.Now.Sub(r.Last) > pastAfter, Evidence: []string{evLine(r)}, Hint: "Pods were restarted by the reboot; find out why it rebooted (kernel panic, maintenance, autoscaler). On the node: `last -x reboot`, `journalctl --list-boots`, `coredumpctl list`."})
		case "NodeNotReady":
			add(Cause{Title: "The node changed to NotReady", Score: 65, Evidence: []string{evLine(r)}})
		}
	}

	if c := nodeStorageCause(ev.BadPods); c != nil {
		add(*c)
	}

	// A failing node-level network agent explains pods stuck in ContainerCreating.
	var cni []NodePod
	stuck := 0
	for _, p := range ev.BadPods {
		if cniPod.MatchString(p.Name) {
			cni = append(cni, p)
		}
		if p.Status == "ContainerCreating" && p.Age > 5*time.Minute {
			stuck++
		}
	}
	if len(cni) > 0 {
		evid := []string{}
		for _, p := range cni {
			evid = append(evid, p.String())
		}
		if stuck > 0 {
			evid = append(evid, fmt.Sprintf("%d other pod(s) on this node are stuck in ContainerCreating, which is what a broken pod network does", stuck))
		}
		add(Cause{Title: "A network (CNI) agent on this node is failing", Score: 89, Evidence: evid,
			Hint: "Without the CNI agent new pods cannot get a network and stay in ContainerCreating. Start with this pod: kubekaze why " + cni[0].Name + " -n " + cni[0].Namespace})
	} else if stuck >= 2 {
		add(Cause{Title: fmt.Sprintf("%d pods have been stuck in ContainerCreating for over 5 minutes", stuck), Score: 60,
			Evidence: []string{"several pods on the same node cannot start their containers"},
			Hint:     "A node-level problem: container runtime, CNI, or a volume/storage backend. Check the runtime and kubelet logs on the node."})
	}

	burst, frequent := restartCauses(ev)
	if burst != nil {
		add(*burst)
	}
	// One container that restarts a lot is that pod's problem, not the node's:
	// it only supports a diagnosis that already exists, it never creates one.
	if frequent != nil && current(causes) {
		add(*frequent)
	}

	if current(causes) && len(ev.BadPods) > 0 {
		shown := make([]string, 0, 5)
		for i, p := range ev.BadPods {
			if i == 5 {
				shown = append(shown, fmt.Sprintf("(and %d more)", len(ev.BadPods)-5))
				break
			}
			shown = append(shown, p.String())
		}
		add(Cause{Title: fmt.Sprintf("%d pod(s) on this node are unhealthy", len(ev.BadPods)), Score: 40,
			Evidence: shown, Hint: "Explain one of them: kubekaze why <pod> -n <namespace>"})
	}

	sort.SliceStable(causes, func(i, j int) bool {
		if causes[i].Past != causes[j].Past {
			return !causes[i].Past // current problems first
		}
		return causes[i].Score > causes[j].Score
	})
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

// restartCauses turns container restart history into causes. The API cannot see
// inside the node (kernel crashes, core dumps, reboots older than the event
// retention), but it does remember when containers were restarted: many pods
// restarting at the same moment points at the node, one pod restarting over and
// over points at that pod.
func restartCauses(ev NodeEvidence) (burst, frequent *Cause) {
	var recent []PodRestart
	for _, r := range ev.Restarts {
		if !r.Last.IsZero() && ev.Now.Sub(r.Last) <= restartWindow {
			recent = append(recent, r)
		}
	}
	if len(recent) == 0 {
		return nil, nil
	}

	// Burst: the latest group of pods whose last runs ended within burstSpan.
	sort.Slice(recent, func(i, j int) bool { return recent[i].Last.Before(recent[j].Last) })
	var best []PodRestart
	for i := range recent {
		pods := map[string]bool{}
		var group []PodRestart
		for j := i; j < len(recent) && recent[j].Last.Sub(recent[i].Last) <= burstSpan; j++ {
			key := recent[j].Namespace + "/" + recent[j].Pod
			pods[key] = true
			group = append(group, recent[j])
		}
		if len(pods) >= burstPods && (len(pods) >= distinctPods(best)) {
			best = group
		}
	}
	if len(best) > 0 {
		last := best[len(best)-1].Last
		evidence := []string{fmt.Sprintf("last of them ended %s (%s ago)", last.Local().Format("2006-01-02 15:04:05 MST"), events.Age(ev.Now.Sub(last)))}
		for i, r := range best {
			if i == 5 {
				evidence = append(evidence, fmt.Sprintf("(and %d more containers)", len(best)-5))
				break
			}
			evidence = append(evidence, fmt.Sprintf("%s: %s at %s", r, exitText(r), r.Last.Local().Format("15:04:05")))
		}
		hint := "Pods that have nothing to do with each other restarting together point at the node: a reboot, a kernel or runtime crash, or an overloaded kubelet. Kubernetes cannot see inside the node; check there around that time: `last -x reboot`, `journalctl --list-boots`, `coredumpctl list`, `dmesg -T`."
		if controlPlaneBurst(best) {
			hint = "Control plane components restart together when they cannot renew their leader lease, usually because the API server or etcd was slow or unreachable. Check the etcd and API server logs around that time (disk latency, \"took too long\", lost leader), then the node itself: `last -x reboot`, `journalctl`, `coredumpctl list`."
		}
		burst = &Cause{
			Title:    fmt.Sprintf("%d pods on this node restarted within %s of each other", distinctPods(best), events.Age(burstSpan)),
			Score:    58,
			Evidence: evidence,
			Hint:     hint,
			Past:     ev.Now.Sub(last) > pastAfter,
		}
	}

	// Frequent restarters.
	var many []PodRestart
	for _, r := range recent {
		if r.Count >= manyRestarts {
			many = append(many, r)
		}
	}
	if len(many) > 0 {
		sort.Slice(many, func(i, j int) bool { return many[i].Count > many[j].Count })
		shown := []string{}
		for i, r := range many {
			if i == 5 {
				shown = append(shown, fmt.Sprintf("(and %d more)", len(many)-5))
				break
			}
			shown = append(shown, fmt.Sprintf("%s: %d restarts, last %s ago", r, r.Count, events.Age(ev.Now.Sub(r.Last))))
		}
		frequent = &Cause{
			Title:    fmt.Sprintf("%d container(s) on this node keep restarting", len(many)),
			Score:    35,
			Evidence: shown,
			Hint:     "Explain the worst one: kubekaze why " + many[0].Pod + " -n " + many[0].Namespace + ". If the same node always shows restarts, compare with the same workload on other nodes.",
		}
	}
	return burst, frequent
}

func distinctPods(rs []PodRestart) int {
	m := map[string]bool{}
	for _, r := range rs {
		m[r.Namespace+"/"+r.Pod] = true
	}
	return len(m)
}

// current reports whether any cause is about the node's present state.
func current(cs []Cause) bool {
	for _, c := range cs {
		if !c.Past {
			return true
		}
	}
	return false
}

func exitText(r PodRestart) string {
	if r.Reason == "" {
		return fmt.Sprintf("exit %d", r.ExitCode)
	}
	return fmt.Sprintf("exit %d %s", r.ExitCode, r.Reason)
}

// controlPlanePrefixes are pod names of components that use leader election or
// back it (static pods are named <component>-<node>).
var controlPlanePrefixes = []string{"kube-apiserver-", "kube-controller-manager-", "kube-scheduler-", "cloud-controller-manager-", "etcd-"}

// controlPlaneBurst reports whether a burst is a control plane event: at least
// two control plane components, and nothing outside the system namespaces.
func controlPlaneBurst(rs []PodRestart) bool {
	cp := map[string]bool{}
	for _, r := range rs {
		if r.Namespace != "kube-system" && !strings.HasPrefix(r.Namespace, "openshift-") {
			return false
		}
		for _, p := range controlPlanePrefixes {
			if strings.HasPrefix(r.Pod, p) {
				cp[r.Pod] = true
			}
		}
	}
	return len(cp) >= 2
}

// nodeStorageCause reports a failing container storage seen through the pods of
// the node: node events usually say nothing about it. One pod is enough for an
// I/O error or a failed overlay mount; "read-only file system" or "no space
// left" from a single pod can be that pod's own volume, so they need two.
func nodeStorageCause(pods []NodePod) *Cause {
	var hit []NodePod
	certain := false
	for _, p := range pods {
		if !storageFailure(p.Message) {
			continue
		}
		hit = append(hit, p)
		low := strings.ToLower(p.Message)
		if strings.Contains(low, "input/output error") || strings.Contains(low, "overlay") {
			certain = true
		}
	}
	if len(hit) == 0 || (!certain && len(hit) < 2) {
		return nil
	}
	evidence := []string{}
	for i, p := range hit {
		if i == 5 {
			evidence = append(evidence, fmt.Sprintf("(and %d more)", len(hit)-5))
			break
		}
		evidence = append(evidence, p.String())
	}
	evidence = append(evidence, Shorten(hit[0].Message, maxEvidence))
	return &Cause{
		Title:    "The node's container storage is failing",
		Score:    93,
		Evidence: evidence,
		Hint:     "The runtime cannot create container filesystems here. On the node check the disk and filesystem (`dmesg -T`, `journalctl -u crio` or `-u containerd`, `df -h`). Drain the node if the disk is failing.",
	}
}
