package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/beomzh/kubekaze/internal/events"
	"github.com/beomzh/kubekaze/internal/podstatus"
	"github.com/beomzh/kubekaze/internal/redact"
	"github.com/beomzh/kubekaze/internal/target"
	"github.com/beomzh/kubekaze/internal/term"
	"github.com/beomzh/kubekaze/internal/why"
)

type whyFlags struct {
	noRedact bool
	logLines int
	max      int
}

func (a *app) newWhyCmd() *cobra.Command {
	var f whyFlags
	cmd := &cobra.Command{
		Use:   "why <pod | deploy/name | sts/name | ds/name | job/name>",
		Short: "Explain why something is not working: timeline, evidence and suspected causes",
		Long: `Explain why a pod (or the unhealthy pods of a workload) is not working.

kubekaze gathers the pod's state, the last termination reason, its events,
the owner chain, recent rollouts and the tail of the failing container's log
(the previous run when it restarted), then ranks suspected causes with rules.
Log lines have common secrets masked unless --no-redact is given.

  kubekaze why api-7d9f
  kubekaze why deploy/api
  kubekaze why pod/api-7d9f -o json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error { return a.runWhy(cmd.Context(), args[0], f) },
	}
	fl := cmd.Flags()
	fl.BoolVar(&f.noRedact, "no-redact", false, "do not mask secrets in log lines")
	fl.IntVar(&f.logLines, "log-lines", 15, "number of log lines to include")
	fl.IntVar(&f.max, "max-pods", 3, "for a workload: how many unhealthy pods to explain")
	return cmd
}

type containerView struct {
	Name     string `json:"name"`
	State    string `json:"state"`
	Restarts int32  `json:"restarts"`
	Last     string `json:"lastTermination,omitempty"`
}

type whyReport struct {
	Namespace  string          `json:"namespace"`
	Pod        string          `json:"pod"`
	Status     string          `json:"status"`
	Age        string          `json:"age"`
	Owners     []string        `json:"owners,omitempty"`
	Containers []containerView `json:"containers"`
	Events     []events.Row    `json:"events"`
	LogSource  string          `json:"logSource,omitempty"`
	Logs       []string        `json:"logs,omitempty"`
	Causes     []why.Cause     `json:"causes"`
	Healthy    bool            `json:"healthy"`
}

func (a *app) runWhy(ctx context.Context, ref string, f whyFlags) error {
	cs, err := a.opts.Clientset()
	if err != nil {
		return err
	}
	ns, err := a.opts.ResolvedNamespace()
	if err != nil {
		return err
	}
	kind, name, err := target.ParseRef(ref)
	if err != nil {
		return err
	}
	var pods []corev1.Pod
	if kind == "pod" {
		p, err := cs.CoreV1().Pods(ns).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("getting pod %s/%s: %w", ns, name, err)
		}
		pods = []corev1.Pod{*p}
	} else {
		sel, err := target.Selector(ctx, cs, ns, kind, name)
		if err != nil {
			return err
		}
		list, err := cs.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{LabelSelector: sel})
		if err != nil {
			return fmt.Errorf("listing pods: %w", err)
		}
		for _, p := range list.Items {
			if !podstatus.Healthy(&p) || podstatus.Restarts(&p) > 0 {
				pods = append(pods, p)
			}
		}
		if len(pods) == 0 {
			if a.output == "json" {
				fmt.Fprintln(a.out, "[]")
				return nil
			}
			fmt.Fprintf(a.out, "%s All %d pod(s) of %s/%s look healthy.\n", a.pal.Green("OK"), len(list.Items), kind, name)
			return nil
		}
		sort.SliceStable(pods, func(i, j int) bool {
			hi, hj := podstatus.Healthy(&pods[i]), podstatus.Healthy(&pods[j])
			if hi != hj {
				return !hi // unhealthy pods first
			}
			return podstatus.Restarts(&pods[i]) > podstatus.Restarts(&pods[j])
		})
		if len(pods) > f.max {
			fmt.Fprintf(a.err, "%s showing %d of %d unhealthy pods (--max-pods)\n", a.pal.Dim("note:"), f.max, len(pods))
			pods = pods[:f.max]
		}
	}

	var reports []*whyReport
	for i := range pods {
		r, err := a.buildWhy(ctx, cs, &pods[i], f)
		if err != nil {
			return err
		}
		reports = append(reports, r)
	}
	if a.output == "json" {
		enc := json.NewEncoder(a.out)
		enc.SetIndent("", "  ")
		return enc.Encode(reports)
	}
	for i, r := range reports {
		if i > 0 {
			fmt.Fprintln(a.out)
		}
		a.printWhy(r)
	}
	for _, r := range reports {
		if !r.Healthy {
			return exitCodeError{code: exitProblems}
		}
	}
	return nil
}

func (a *app) buildWhy(ctx context.Context, cs kubernetes.Interface, pod *corev1.Pod, f whyFlags) (*whyReport, error) {
	now := time.Now()
	r := &whyReport{
		Namespace: pod.Namespace, Pod: pod.Name, Status: podstatus.Of(pod),
		Age: events.Age(now.Sub(pod.CreationTimestamp.Time)),
	}
	owners, rolloutAge := a.ownerChain(ctx, cs, pod, now)
	r.Owners = owners

	for _, c := range pod.Status.ContainerStatuses {
		v := containerView{Name: c.Name, Restarts: c.RestartCount, State: describeState(c.State)}
		if t := c.LastTerminationState.Terminated; t != nil {
			v.Last = fmt.Sprintf("%s (exit %d) %s ago", orDash(t.Reason), t.ExitCode, events.Age(now.Sub(t.FinishedAt.Time)))
		}
		r.Containers = append(r.Containers, v)
	}

	evRows, _, err := fetchEvents(ctx, cs, []string{pod.Namespace}, "involvedObject.name="+pod.Name)
	if err == nil {
		evRows = events.Filter{Kind: "pod", Name: pod.Name}.Apply(evRows)
		evRows = events.Collapse(evRows)
		events.Sort(evRows, "time", false)
		r.Events = evRows
	}

	r.LogSource, r.Logs = a.failingLogs(ctx, cs, pod, f)
	r.Causes = why.Diagnose(why.Evidence{Pod: pod, Events: r.Events, LogTail: r.Logs, RolloutAge: rolloutAge, Now: now})
	r.Healthy = len(r.Causes) == 0
	return r, nil
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func describeState(s corev1.ContainerState) string {
	switch {
	case s.Running != nil:
		return "Running"
	case s.Waiting != nil:
		return "Waiting: " + s.Waiting.Reason
	case s.Terminated != nil:
		return "Terminated: " + orDash(s.Terminated.Reason)
	}
	return "-"
}

// ownerChain follows controller owners and, for Deployments, reports how
// long ago the newest ReplicaSet was created.
func (a *app) ownerChain(ctx context.Context, cs kubernetes.Interface, pod *corev1.Pod, now time.Time) ([]string, time.Duration) {
	var chain []string
	var rollout time.Duration
	owner := metav1.GetControllerOf(pod)
	for depth := 0; owner != nil && depth < 4; depth++ {
		chain = append(chain, owner.Kind+"/"+owner.Name)
		switch owner.Kind {
		case "ReplicaSet":
			rs, err := cs.AppsV1().ReplicaSets(pod.Namespace).Get(ctx, owner.Name, metav1.GetOptions{})
			if err != nil {
				return chain, rollout
			}
			owner = metav1.GetControllerOf(rs)
			if owner != nil && owner.Kind == "Deployment" {
				if age, ok := a.newestRSAge(ctx, cs, pod.Namespace, owner.Name, now); ok {
					rollout = age
				}
			}
		case "Job":
			j, err := cs.BatchV1().Jobs(pod.Namespace).Get(ctx, owner.Name, metav1.GetOptions{})
			if err != nil {
				return chain, rollout
			}
			owner = metav1.GetControllerOf(j)
		default:
			return chain, rollout
		}
	}
	return chain, rollout
}

func (a *app) newestRSAge(ctx context.Context, cs kubernetes.Interface, ns, deploy string, now time.Time) (time.Duration, bool) {
	d, err := cs.AppsV1().Deployments(ns).Get(ctx, deploy, metav1.GetOptions{})
	if err != nil || d.Spec.Selector == nil {
		return 0, false
	}
	sel, err := metav1.LabelSelectorAsSelector(d.Spec.Selector)
	if err != nil {
		return 0, false
	}
	list, err := cs.AppsV1().ReplicaSets(ns).List(ctx, metav1.ListOptions{LabelSelector: sel.String()})
	if err != nil || len(list.Items) == 0 {
		return 0, false
	}
	newest := list.Items[0].CreationTimestamp.Time
	for _, rs := range list.Items {
		if rs.CreationTimestamp.Time.After(newest) {
			newest = rs.CreationTimestamp.Time
		}
	}
	return now.Sub(newest), true
}

// failingLogs returns the log tail of the container most likely to explain the
// problem: previous run if it restarted, else the current run.
func (a *app) failingLogs(ctx context.Context, cs kubernetes.Interface, pod *corev1.Pod, f whyFlags) (string, []string) {
	var pick *corev1.ContainerStatus
	for i := range pod.Status.ContainerStatuses {
		c := &pod.Status.ContainerStatuses[i]
		if c.RestartCount > 0 || !c.Ready {
			pick = c
			break
		}
	}
	if pick == nil {
		return "", nil
	}
	lines := int64(f.logLines)
	try := func(previous bool) []string {
		req := cs.CoreV1().Pods(pod.Namespace).GetLogs(pod.Name, &corev1.PodLogOptions{Container: pick.Name, Previous: previous, TailLines: &lines})
		stream, err := req.Stream(ctx)
		if err != nil {
			return nil
		}
		defer stream.Close()
		var out []string
		sc := bufio.NewScanner(stream)
		sc.Buffer(make([]byte, 64*1024), 1024*1024)
		for sc.Scan() {
			l := sc.Text()
			if !f.noRedact {
				l = redact.Line(l)
			}
			out = append(out, l)
		}
		return out
	}
	if pick.RestartCount > 0 {
		if l := try(true); len(l) > 0 {
			return fmt.Sprintf("previous run of container %q", pick.Name), l
		}
	}
	if l := try(false); len(l) > 0 {
		return fmt.Sprintf("current run of container %q", pick.Name), l
	}
	return "", nil
}

func (a *app) printWhy(r *whyReport) {
	st := r.Status
	if r.Healthy {
		st = a.pal.Green(st)
	} else {
		st = a.pal.Red(st)
	}
	fmt.Fprintf(a.out, "%s %s/%s   %s   age %s\n", a.pal.Bold("Pod"), term.SafeText(r.Namespace), term.SafeText(r.Pod), st, r.Age)
	if len(r.Owners) > 0 {
		fmt.Fprintf(a.out, "Owner: %s\n", term.SafeText(strings.Join(r.Owners, " -> ")))
	}
	fmt.Fprintln(a.out)

	fmt.Fprintln(a.out, a.pal.Bold("Containers"))
	rows := make([][]cell, 0, len(r.Containers))
	for _, c := range r.Containers {
		rows = append(rows, []cell{plain(c.Name), plain(c.State), plain(fmt.Sprint(c.Restarts)), plain(c.Last)})
	}
	table(a.out, []string{"NAME", "STATE", "RESTARTS", "LAST TERMINATION"}, rows)

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

	if len(r.Logs) > 0 {
		fmt.Fprintln(a.out)
		fmt.Fprintf(a.out, "%s (%s)\n", a.pal.Bold("Last log lines"), term.SafeText(r.LogSource))
		for _, l := range r.Logs {
			fmt.Fprintln(a.out, "  "+term.SafeText(l))
		}
	}

	fmt.Fprintln(a.out)
	if r.Healthy {
		fmt.Fprintf(a.out, "%s No problem found for this pod.\n", a.pal.Green("OK"))
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
