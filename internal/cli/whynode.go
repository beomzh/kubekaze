package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/beomzh/kubekaze/internal/events"
	"github.com/beomzh/kubekaze/internal/podstatus"
	"github.com/beomzh/kubekaze/internal/status"
	"github.com/beomzh/kubekaze/internal/term"
	"github.com/beomzh/kubekaze/internal/why"
)

type nodeReport struct {
	Node       string          `json:"node"`
	Status     string          `json:"status"`
	Age        string          `json:"age"`
	Kubelet    string          `json:"kubeletVersion"`
	Conditions []nodeCondition `json:"conditions"`
	Taints     []string        `json:"taints,omitempty"`
	Pods       int             `json:"pods"`
	BadPods    []string        `json:"unhealthyPods,omitempty"`
	Events     []events.Row    `json:"events"`
	Causes     []why.Cause     `json:"causes"`
	Healthy    bool            `json:"healthy"`
}

type nodeCondition struct {
	Type      string `json:"type"`
	Status    string `json:"status"`
	Reason    string `json:"reason,omitempty"`
	Message   string `json:"message,omitempty"`
	Heartbeat string `json:"lastHeartbeat,omitempty"`
}

func nodeStatus(n *corev1.Node, now time.Time) string {
	st := "Unknown"
	for _, c := range n.Status.Conditions {
		if c.Type != corev1.NodeReady {
			continue
		}
		switch c.Status {
		case corev1.ConditionTrue:
			st = "Ready"
			if !c.LastHeartbeatTime.IsZero() && now.Sub(c.LastHeartbeatTime.Time) > status.HeartbeatStale {
				st = "Unknown"
			}
		case corev1.ConditionFalse:
			st = "NotReady"
		}
	}
	if n.Spec.Unschedulable {
		st += ",SchedulingDisabled"
	}
	return st
}

// isAllNodes reports whether ref means "every node": a bare node, nodes or no.
// A pod really called "node" can still be reached as pod/node.
func isAllNodes(ref string) bool {
	switch strings.ToLower(ref) {
	case "node", "nodes", "no":
		return true
	}
	return false
}

// nodeReportFor builds the report of one node from data that was already loaded.
func nodeReportFor(n *corev1.Node, evs []events.Row, pods []corev1.Pod, now time.Time) *nodeReport {
	r := &nodeReport{
		Node: n.Name, Status: nodeStatus(n, now), Age: events.Age(now.Sub(n.CreationTimestamp.Time)),
		Kubelet: n.Status.NodeInfo.KubeletVersion, Pods: len(pods),
	}
	for _, c := range n.Status.Conditions {
		nc := nodeCondition{Type: string(c.Type), Status: string(c.Status), Reason: c.Reason, Message: c.Message}
		if !c.LastHeartbeatTime.IsZero() {
			nc.Heartbeat = events.Age(now.Sub(c.LastHeartbeatTime.Time)) + " ago"
		}
		r.Conditions = append(r.Conditions, nc)
	}
	for _, t := range n.Spec.Taints {
		r.Taints = append(r.Taints, fmt.Sprintf("%s:%s", t.Key, t.Effect))
	}
	r.Events = events.Collapse(evs)
	events.Sort(r.Events, "time", false)
	var bad []why.NodePod
	for i := range pods {
		p := &pods[i]
		st := podstatus.Of(p)
		age := now.Sub(p.CreationTimestamp.Time)
		starting := (st == "Pending" || st == "ContainerCreating" || st == "PodInitializing") && age < 2*time.Minute
		if !podstatus.Healthy(p) && !starting {
			bad = append(bad, why.NodePod{Namespace: p.Namespace, Name: p.Name, Status: st, Age: age, Message: failureMessage(p)})
		}
	}
	sort.Slice(bad, func(i, j int) bool {
		if bad[i].Namespace != bad[j].Namespace {
			return bad[i].Namespace < bad[j].Namespace
		}
		return bad[i].Name < bad[j].Name
	})
	for _, b := range bad {
		r.BadPods = append(r.BadPods, fmt.Sprintf("%s/%s: %s", b.Namespace, b.Name, b.Status))
	}
	var restarts []why.PodRestart
	for i := range pods {
		p := &pods[i]
		for _, cs := range p.Status.ContainerStatuses {
			if cs.RestartCount > 0 && cs.LastTerminationState.Terminated != nil {
				restarts = append(restarts, why.PodRestart{
					Namespace: p.Namespace, Pod: p.Name, Container: cs.Name,
					Count: cs.RestartCount, Last: cs.LastTerminationState.Terminated.FinishedAt.Time,
					ExitCode: cs.LastTerminationState.Terminated.ExitCode, Reason: cs.LastTerminationState.Terminated.Reason,
				})
			}
		}
	}
	r.Causes = why.DiagnoseNode(why.NodeEvidence{Node: n, Events: r.Events, BadPods: bad, Restarts: restarts, Now: now})
	r.Healthy = len(currentCauses(r.Causes)) == 0
	return r
}

// failureMessage returns why a container of the pod is not running, preferring
// a message that points at the node's storage.
func failureMessage(p *corev1.Pod) string {
	first := ""
	all := append(append([]corev1.ContainerStatus{}, p.Status.InitContainerStatuses...), p.Status.ContainerStatuses...)
	for _, cs := range all {
		for _, st := range []corev1.ContainerState{cs.State, cs.LastTerminationState} {
			msg := ""
			if st.Waiting != nil {
				msg = st.Waiting.Message
			} else if st.Terminated != nil && st.Terminated.ExitCode != 0 && st.Terminated.Reason != "Error" {
				// "Error" carries the application's own output, not the runtime's
				msg = st.Terminated.Message
			}
			if msg == "" {
				continue
			}
			if strings.Contains(strings.ToLower(msg), "input/output error") {
				return msg
			}
			if first == "" {
				first = msg
			}
		}
	}
	return first
}

// loadNodeEvents returns node events grouped by node name. Node events usually
// live in the "default" namespace, so every namespace is listed. name may be
// empty for all nodes.
func loadNodeEvents(ctx context.Context, cs kubernetes.Interface, name string) map[string][]events.Row {
	sel := "involvedObject.kind=Node"
	if name != "" {
		sel += ",involvedObject.name=" + name
	}
	out := map[string][]events.Row{}
	l, err := cs.CoreV1().Events(metav1.NamespaceAll).List(ctx, metav1.ListOptions{FieldSelector: sel})
	if err != nil {
		return out
	}
	for _, e := range l.Items {
		out[e.InvolvedObject.Name] = append(out[e.InvolvedObject.Name], events.FromCore(e))
	}
	return out
}

func (a *app) emitNodeReports(reports []*nodeReport, single bool) error {
	if a.output == "json" {
		enc := json.NewEncoder(a.out)
		enc.SetIndent("", "  ")
		if single {
			return enc.Encode(reports[0])
		}
		return enc.Encode(reports)
	}
	bad, okNames := 0, []string{}
	for _, r := range reports {
		if single {
			a.printNode(r) // a node you asked about by name is always shown in full
			continue
		}
		if r.Healthy {
			okNames = append(okNames, r.Node)
			continue
		}
		if bad > 0 {
			fmt.Fprintln(a.out)
		}
		bad++
		a.printNode(r)
	}
	if single {
		return nil
	}
	if bad > 0 {
		fmt.Fprintln(a.out)
	}
	if bad == 0 {
		fmt.Fprintf(a.out, "%s All %d node(s) look healthy: %s\n", a.pal.Green("OK"), len(reports), term.SafeText(strings.Join(okNames, ", ")))
		a.printPastSummary(reports)
		return nil
	}
	fmt.Fprintf(a.out, "%d of %d node(s) have problems.", bad, len(reports))
	if len(okNames) > 0 {
		fmt.Fprintf(a.out, " Healthy: %s", term.SafeText(strings.Join(okNames, ", ")))
	}
	fmt.Fprintln(a.out)
	a.printPastSummary(reports)
	return nil
}

func anyUnhealthy(reports []*nodeReport) bool {
	for _, r := range reports {
		if !r.Healthy {
			return true
		}
	}
	return false
}

// whyNode explains one node.
func (a *app) whyNode(ctx context.Context, cs kubernetes.Interface, name string) error {
	n, err := cs.CoreV1().Nodes().Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("getting node %s: %w", name, err)
	}
	var pods []corev1.Pod
	if pl, err := cs.CoreV1().Pods(metav1.NamespaceAll).List(ctx, metav1.ListOptions{FieldSelector: "spec.nodeName=" + name}); err == nil {
		pods = pl.Items
	}
	reports := []*nodeReport{nodeReportFor(n, loadNodeEvents(ctx, cs, name)[name], pods, time.Now())}
	if err := a.emitNodeReports(reports, true); err != nil {
		return err
	}
	if anyUnhealthy(reports) {
		return exitCodeError{code: exitProblems}
	}
	return nil
}

// whyAllNodes checks every node and prints the ones with problems.
func (a *app) whyAllNodes(ctx context.Context, cs kubernetes.Interface) error {
	nl, err := cs.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("listing nodes (you may not be allowed to see nodes on this cluster): %w", err)
	}
	if len(nl.Items) == 0 {
		return fmt.Errorf("no nodes found")
	}
	byNode := map[string][]corev1.Pod{}
	if pl, err := cs.CoreV1().Pods(metav1.NamespaceAll).List(ctx, metav1.ListOptions{}); err == nil {
		for _, p := range pl.Items {
			byNode[p.Spec.NodeName] = append(byNode[p.Spec.NodeName], p)
		}
	}
	evs := loadNodeEvents(ctx, cs, "")
	now := time.Now()
	reports := make([]*nodeReport, 0, len(nl.Items))
	for i := range nl.Items {
		n := &nl.Items[i]
		reports = append(reports, nodeReportFor(n, evs[n.Name], byNode[n.Name], now))
	}
	sort.SliceStable(reports, func(i, j int) bool {
		if reports[i].Healthy != reports[j].Healthy {
			return !reports[i].Healthy
		}
		return reports[i].Node < reports[j].Node
	})
	if err := a.emitNodeReports(reports, false); err != nil {
		return err
	}
	if anyUnhealthy(reports) {
		return exitCodeError{code: exitProblems}
	}
	return nil
}

func (a *app) printNode(r *nodeReport) {
	st := r.Status
	if r.Healthy {
		st = a.pal.Green(st)
	} else {
		st = a.pal.Red(st)
	}
	fmt.Fprintf(a.out, "%s %s   %s   kubelet %s   age %s   %d pod(s)\n", a.pal.Bold("Node"), term.SafeText(r.Node), st, term.SafeText(r.Kubelet), r.Age, r.Pods)
	if len(r.Taints) > 0 {
		fmt.Fprintf(a.out, "Taints: %s\n", term.SafeText(joinStrings(r.Taints, ", ")))
	}
	fmt.Fprintln(a.out)
	fmt.Fprintln(a.out, a.pal.Bold("Conditions"))
	rows := make([][]cell, 0, len(r.Conditions))
	for _, c := range r.Conditions {
		stc := plain(c.Status)
		bad := (c.Type == "Ready" && c.Status != "True") || (c.Type != "Ready" && c.Status == "True")
		if bad {
			stc.style = a.pal.Red
		}
		rows = append(rows, []cell{plain(c.Type), stc, plain(c.Reason), plain(c.Heartbeat), plain(c.Message)})
	}
	table(a.out, []string{"TYPE", "STATUS", "REASON", "HEARTBEAT", "MESSAGE"}, rows)

	if len(r.Events) > 0 {
		fmt.Fprintln(a.out)
		fmt.Fprintln(a.out, a.pal.Bold("Timeline (oldest first)"))
		show := r.Events
		if len(show) > 12 {
			show = show[len(show)-12:]
		}
		now := time.Now()
		var erows [][]cell
		for _, e := range show {
			erows = append(erows, a.eventCells(e, false, now))
		}
		table(a.out, []string{"LAST SEEN", "TYPE", "REASON", "OBJECT", "MESSAGE"}, erows)
	}
	fmt.Fprintln(a.out)
	if r.Healthy {
		fmt.Fprintf(a.out, "%s No problem found for this node.\n", a.pal.Green("OK"))
		fmt.Fprintln(a.out, a.pal.Dim("Checked through the Kubernetes API only. Crashes inside the node (core dumps, kernel messages) are not visible from here: on the node run `coredumpctl list` and `dmesg -T`."))
	} else {
		fmt.Fprintln(a.out, a.pal.Bold("Suspected causes (most likely first)"))
		a.printCauses(currentCauses(r.Causes))
	}
	if past := pastCauses(r.Causes); len(past) > 0 {
		fmt.Fprintln(a.out)
		fmt.Fprintln(a.out, a.pal.Bold("Past events (nothing repeated since; not counted as a current problem)"))
		a.printCauses(past)
	}
}

func (a *app) printCauses(causes []why.Cause) {
	for i, c := range causes {
		fmt.Fprintf(a.out, "%d. %s\n", i+1, a.pal.Yellow(term.SafeText(c.Title)))
		for _, e := range c.Evidence {
			fmt.Fprintf(a.out, "     - %s\n", term.SafeText(e))
		}
		if c.Hint != "" {
			fmt.Fprintf(a.out, "     %s %s\n", a.pal.Cyan("->"), term.SafeText(c.Hint))
		}
	}
}

func currentCauses(cs []why.Cause) []why.Cause { return causesByPast(cs, false) }

func pastCauses(cs []why.Cause) []why.Cause { return causesByPast(cs, true) }

func causesByPast(cs []why.Cause, past bool) []why.Cause {
	var out []why.Cause
	for _, c := range cs {
		if c.Past == past {
			out = append(out, c)
		}
	}
	return out
}

// printPastSummary mentions past events of nodes that are healthy now, so that
// "all healthy" does not hide them.
func (a *app) printPastSummary(reports []*nodeReport) {
	for _, r := range reports {
		if !r.Healthy {
			continue // shown in full already
		}
		for _, c := range pastCauses(r.Causes) {
			detail := ""
			if len(c.Evidence) > 0 {
				detail = " (" + c.Evidence[0] + ")"
			}
			fmt.Fprintln(a.out, a.pal.Dim(term.SafeText(fmt.Sprintf("Past event on %s: %s%s. Details: kubekaze why node/%s", r.Node, c.Title, detail, r.Node))))
		}
	}
}

func joinStrings(s []string, sep string) string {
	out := ""
	for i, x := range s {
		if i > 0 {
			out += sep
		}
		out += x
	}
	return out
}
