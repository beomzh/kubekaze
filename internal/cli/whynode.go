package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/beomzh/kubekaze/internal/events"
	"github.com/beomzh/kubekaze/internal/podstatus"
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
			if !c.LastHeartbeatTime.IsZero() && now.Sub(c.LastHeartbeatTime.Time) > 2*time.Minute {
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

func (a *app) whyNode(ctx context.Context, cs kubernetes.Interface, name string) error {
	n, err := cs.CoreV1().Nodes().Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("getting node %s: %w", name, err)
	}
	now := time.Now()
	r := &nodeReport{
		Node: n.Name, Status: nodeStatus(n, now), Age: events.Age(now.Sub(n.CreationTimestamp.Time)),
		Kubelet: n.Status.NodeInfo.KubeletVersion,
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

	// Events: node events usually live in the "default" namespace, so list all.
	evl, err := cs.CoreV1().Events(metav1.NamespaceAll).List(ctx, metav1.ListOptions{
		FieldSelector: "involvedObject.kind=Node,involvedObject.name=" + name,
	})
	if err == nil {
		for _, e := range evl.Items {
			r.Events = append(r.Events, events.FromCore(e))
		}
		r.Events = events.Collapse(r.Events)
		events.Sort(r.Events, "time", false)
	}

	// Pods running on the node (cluster-wide list; skipped when not permitted).
	if pl, err := cs.CoreV1().Pods(metav1.NamespaceAll).List(ctx, metav1.ListOptions{FieldSelector: "spec.nodeName=" + name}); err == nil {
		r.Pods = len(pl.Items)
		for i := range pl.Items {
			p := &pl.Items[i]
			st := podstatus.Of(p)
			starting := (st == "Pending" || st == "ContainerCreating" || st == "PodInitializing") && now.Sub(p.CreationTimestamp.Time) < 2*time.Minute
			if !podstatus.Healthy(p) && !starting {
				r.BadPods = append(r.BadPods, fmt.Sprintf("%s/%s: %s", p.Namespace, p.Name, st))
			}
		}
		sort.Strings(r.BadPods)
	}

	r.Causes = why.DiagnoseNode(why.NodeEvidence{Node: n, Events: r.Events, BadPods: r.BadPods, Now: now})
	r.Healthy = len(r.Causes) == 0

	if a.output == "json" {
		enc := json.NewEncoder(a.out)
		enc.SetIndent("", "  ")
		if err := enc.Encode(r); err != nil {
			return err
		}
	} else {
		a.printNode(r)
	}
	if !r.Healthy {
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
		return
	}
	fmt.Fprintln(a.out, a.pal.Bold("Suspected causes (most likely first)"))
	for i, c := range r.Causes {
		fmt.Fprintf(a.out, "%d. %s\n", i+1, a.pal.Yellow(term.SafeText(c.Title)))
		for _, e := range c.Evidence {
			fmt.Fprintf(a.out, "     - %s\n", term.SafeText(e))
		}
		if c.Hint != "" {
			fmt.Fprintf(a.out, "     %s %s\n", a.pal.Cyan("->"), term.SafeText(c.Hint))
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
