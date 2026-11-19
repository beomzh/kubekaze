package cli

import (
	"bufio"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/beomzh/kubekaze/internal/events"
	"github.com/beomzh/kubekaze/internal/kube"
	"github.com/beomzh/kubekaze/internal/podstatus"
	"github.com/beomzh/kubekaze/internal/rm"
	"github.com/beomzh/kubekaze/internal/term"
	"github.com/beomzh/kubekaze/internal/ui"
)

// manyThreshold is the pod count from which the user must type the count.
const manyThreshold = 10

type rmFlags struct {
	selector   string
	statuses   []string
	olderThan  string
	node       string
	yes        bool
	dryRun     bool
	max        int
	grace      int64
	confirmCtx string
}

func (a *app) newRmCmd() *cobra.Command {
	var f rmFlags
	cmd := &cobra.Command{
		Use:   "rm pods [name-or-glob...]",
		Short: "Delete many pods at once, with preview and confirmation",
		Long: `Delete many pods without chaining get | grep | awk | xargs.

Select pods by name or glob, label, status, age or node. kubekaze always shows
what will be deleted first and asks for confirmation. Each pod is deleted by
UID, so a pod that was recreated under the same name in the meantime is not
touched.

  kubekaze rm pods --status CrashLoopBackOff,Evicted,Error
  kubekaze rm pods 'api-*' --older-than 2h
  kubekaze rm pods -l app=web --dry-run
  kubekaze rm pods -A --status Evicted --yes
  kubekaze rm pods                       pick from a list (terminal only)

Safety: more than ` + fmt.Sprint(manyThreshold) + ` pods needs the count typed; a production-looking context needs the
context name typed (or --confirm-context=<name> together with --yes); and
--max (default 50) caps one run even with --yes.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			switch strings.ToLower(args[0]) {
			case "pods", "pod", "po":
			default:
				return fmt.Errorf("only pods are supported for now, got %q", args[0])
			}
			cs, err := a.opts.Clientset()
			if err != nil {
				return err
			}
			ctxName, err := a.opts.ContextName()
			if err != nil {
				return err
			}
			var namespaces []string
			if a.opts.AllNamespaces {
				namespaces = []string{metav1.NamespaceAll}
			} else {
				ns, err := a.opts.ResolvedNamespace()
				if err != nil {
					return err
				}
				namespaces = []string{ns}
			}
			return a.runRm(cmd.Context(), cs, ctxName, namespaces, args[1:], f)
		},
	}
	fl := cmd.Flags()
	fl.StringVarP(&f.selector, "selector", "l", "", "label selector")
	fl.StringSliceVar(&f.statuses, "status", nil, "pod status, e.g. CrashLoopBackOff,Evicted,Error,Completed,Pending")
	fl.StringVar(&f.olderThan, "older-than", "", "only pods older than this (30m, 2h, 1d)")
	fl.StringVar(&f.node, "node", "", "only pods on this node")
	fl.BoolVarP(&f.yes, "yes", "y", false, "do not ask for confirmation (for scripts)")
	fl.BoolVar(&f.dryRun, "dry-run", false, "only show what would be deleted")
	fl.IntVar(&f.max, "max", 50, "refuse to delete more than this many pods in one run")
	fl.Int64Var(&f.grace, "grace-period", -1, "seconds to wait before killing; -1 uses each pod's own setting")
	fl.StringVar(&f.confirmCtx, "confirm-context", "", "with --yes on a production-looking context: the context name, to confirm")
	return cmd
}

// confirmation describes what the user must type to proceed.
func confirmation(count int, prod bool, ctxName string) (prompt, expect string) {
	switch {
	case prod:
		return fmt.Sprintf("This is a production-looking context. Type the context name (%s) to delete %d pod(s): ", ctxName, count), ctxName
	case count >= manyThreshold:
		return fmt.Sprintf("Type %d to delete these %d pods: ", count, count), fmt.Sprint(count)
	default:
		return fmt.Sprintf("Delete %d pod(s)? [y/N]: ", count), "y"
	}
}

func (a *app) runRm(ctx context.Context, cs kubernetes.Interface, ctxName string, namespaces []string, patterns []string, f rmFlags) error {
	crit := rm.Criteria{Patterns: patterns, Statuses: f.statuses, Node: f.node, Now: time.Now()}
	if f.olderThan != "" {
		d, err := events.ParseSince(f.olderThan)
		if err != nil || d <= 0 {
			return fmt.Errorf("invalid --older-than %q (examples: 30m, 2h, 1d)", f.olderThan)
		}
		crit.OlderThan = d
	}
	if err := crit.Validate(); err != nil {
		return err
	}
	if f.max < 1 {
		return fmt.Errorf("--max must be at least 1")
	}

	selectorEmpty := f.selector == ""
	if crit.IsEmpty() && selectorEmpty && !a.interactive {
		return fmt.Errorf("no selection given: pass names, -l, --status, --older-than or --node (or run in a terminal to pick from a list)")
	}

	var all []corev1.Pod
	for _, ns := range namespaces {
		list, err := cs.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{LabelSelector: f.selector})
		if err != nil {
			return fmt.Errorf("listing pods: %w", err)
		}
		all = append(all, list.Items...)
	}
	pods := rm.Select(all, crit)

	showNS := len(namespaces) > 1 || namespaces[0] == metav1.NamespaceAll
	if crit.IsEmpty() && selectorEmpty { // interactive pick
		picked, err := a.pickPods(pods, showNS)
		if err != nil {
			return err
		}
		pods = picked
	}
	if len(pods) == 0 {
		fmt.Fprintln(a.err, a.pal.Dim("No pods match."))
		return nil
	}

	prod := kube.IsProduction(ctxName)
	where := fmt.Sprintf("context %s", ctxName)
	if prod {
		where = a.pal.Red(where + " (production?)")
	} else {
		where = a.pal.Bold(where)
	}
	fmt.Fprintf(a.err, "%d pod(s) selected in %s\n", len(pods), where)
	a.printPodPreview(pods, showNS)

	if len(pods) > f.max {
		return fmt.Errorf("refusing to delete %d pods in one run (--max is %d); narrow the selection or raise --max", len(pods), f.max)
	}
	if f.dryRun {
		fmt.Fprintln(a.err, a.pal.Dim("Dry run: nothing deleted."))
		return nil
	}

	if f.yes {
		if prod && f.confirmCtx != ctxName {
			return fmt.Errorf("production-looking context %q: with --yes you must also pass --confirm-context=%s", ctxName, ctxName)
		}
	} else {
		if !a.interactive {
			return fmt.Errorf("not a terminal: refusing to delete without confirmation; pass --yes (and --confirm-context on production)")
		}
		prompt, expect := confirmation(len(pods), prod, ctxName)
		fmt.Fprint(a.err, prompt)
		line, _ := bufio.NewReader(a.in).ReadString('\n')
		got := strings.TrimSpace(line)
		if expect == "y" {
			got = strings.ToLower(got)
			if got == "yes" {
				got = "y"
			}
		}
		if got != expect {
			fmt.Fprintln(a.err, "Aborted. Nothing deleted.")
			return ui.ErrCancelled
		}
	}

	return a.deletePods(ctx, cs, pods, f.grace, showNS)
}

func podLabel(p corev1.Pod, showNS bool, now time.Time) string {
	name := p.Name
	if showNS {
		name = p.Namespace + "/" + p.Name
	}
	return fmt.Sprintf("%s  %s  %s", term.SafeText(name), podstatus.Of(&p), events.Age(now.Sub(p.CreationTimestamp.Time)))
}

func (a *app) pickPods(pods []corev1.Pod, showNS bool) ([]corev1.Pod, error) {
	if len(pods) == 0 {
		return nil, nil
	}
	now := time.Now()
	labels := make([]string, len(pods))
	byLabel := map[string]corev1.Pod{}
	for i, p := range pods {
		labels[i] = podLabel(p, showNS, now)
		byLabel[labels[i]] = p
	}
	chosen, err := ui.PickMany(labels, "pods (Tab to mark, Enter to confirm)")
	if err != nil {
		return nil, err
	}
	out := make([]corev1.Pod, 0, len(chosen))
	for _, l := range chosen {
		out = append(out, byLabel[l])
	}
	return out, nil
}

func (a *app) printPodPreview(pods []corev1.Pod, showNS bool) {
	header := []string{"NAME", "STATUS", "RESTARTS", "AGE", "NODE"}
	if showNS {
		header = append([]string{"NAMESPACE"}, header...)
	}
	now := time.Now()
	rows := make([][]cell, 0, len(pods))
	for i := range pods {
		p := &pods[i]
		st := plain(podstatus.Of(p))
		if !podstatus.Healthy(p) {
			st.style = a.pal.Yellow
		}
		r := []cell{plain(p.Name), st, plain(fmt.Sprint(podstatus.Restarts(p))), plain(events.Age(now.Sub(p.CreationTimestamp.Time))), plain(p.Spec.NodeName)}
		if showNS {
			r = append([]cell{plain(p.Namespace)}, r...)
		}
		rows = append(rows, r)
	}
	table(a.err, header, rows)
}

// deletePods deletes each pod by UID and reports every result.
func (a *app) deletePods(ctx context.Context, cs kubernetes.Interface, pods []corev1.Pod, grace int64, showNS bool) error {
	ok, failed := 0, 0
	for _, p := range pods {
		uid := p.UID
		opts := metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}}
		if grace >= 0 {
			g := grace
			opts.GracePeriodSeconds = &g
		}
		name := p.Namespace + "/" + p.Name
		if err := cs.CoreV1().Pods(p.Namespace).Delete(ctx, p.Name, opts); err != nil {
			failed++
			fmt.Fprintf(a.out, "%s %s: %s\n", a.pal.Red("failed "), term.SafeText(name), term.SafeText(err.Error()))
			continue
		}
		ok++
		fmt.Fprintf(a.out, "%s %s\n", a.pal.Green("deleted"), term.SafeText(name))
	}
	fmt.Fprintf(a.out, "%d deleted, %d failed\n", ok, failed)
	if failed > 0 {
		return fmt.Errorf("%d of %d pods could not be deleted", failed, len(pods))
	}
	return nil
}
