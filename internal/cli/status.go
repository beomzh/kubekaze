package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/spf13/cobra"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/beomzh/kubekaze/internal/status"
)

// exitProblems is returned by "status" when problems were found, so scripts
// can tell "unhealthy" (3) from "error" (1).
const exitProblems = 3

func (a *app) newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show only what is unhealthy in a namespace or the whole cluster",
		Long: `Show only what is unhealthy: failing pods, workloads below their desired
replicas, NotReady nodes, failed jobs and stuck volume claims. Healthy objects
are hidden. Exit code: 0 healthy, 3 problems found, 1 error.

  kubekaze status          current namespace
  kubekaze status -A       whole cluster (nodes included when permitted)
  kubekaze status -A -o json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error { return a.runStatus(cmd.Context()) },
	}
}

type listFn func() error

func (a *app) collect(ctx context.Context, cs kubernetes.Interface, ns string, all bool) (status.Input, []string) {
	in := status.Input{Now: time.Now()}
	var skipped []string
	run := func(what string, fn listFn) {
		if err := fn(); err != nil {
			if apierrors.IsForbidden(err) {
				skipped = append(skipped, what+" (forbidden)")
				return
			}
			skipped = append(skipped, fmt.Sprintf("%s (%v)", what, err))
		}
	}
	lo := metav1.ListOptions{}
	run("pods", func() error {
		l, err := cs.CoreV1().Pods(ns).List(ctx, lo)
		if err == nil {
			in.Pods = l.Items
		}
		return err
	})
	run("deployments", func() error {
		l, err := cs.AppsV1().Deployments(ns).List(ctx, lo)
		if err == nil {
			in.Deployments = l.Items
		}
		return err
	})
	run("statefulsets", func() error {
		l, err := cs.AppsV1().StatefulSets(ns).List(ctx, lo)
		if err == nil {
			in.StatefulSets = l.Items
		}
		return err
	})
	run("daemonsets", func() error {
		l, err := cs.AppsV1().DaemonSets(ns).List(ctx, lo)
		if err == nil {
			in.DaemonSets = l.Items
		}
		return err
	})
	run("jobs", func() error {
		l, err := cs.BatchV1().Jobs(ns).List(ctx, lo)
		if err == nil {
			in.Jobs = l.Items
		}
		return err
	})
	run("persistentvolumeclaims", func() error {
		l, err := cs.CoreV1().PersistentVolumeClaims(ns).List(ctx, lo)
		if err == nil {
			in.PVCs = l.Items
		}
		return err
	})
	if all {
		run("nodes", func() error {
			l, err := cs.CoreV1().Nodes().List(ctx, lo)
			if err == nil {
				in.Nodes = l.Items
			}
			return err
		})
	}
	return in, skipped
}

func (a *app) runStatus(ctx context.Context) error {
	cs, err := a.opts.Clientset()
	if err != nil {
		return err
	}
	ns, err := a.opts.NamespaceOrAll()
	if err != nil {
		return err
	}
	scope := ns
	if a.opts.AllNamespaces {
		scope = "all namespaces"
	}
	in, skipped := a.collect(ctx, cs, ns, a.opts.AllNamespaces)
	findings := status.Analyze(in)

	if a.output == "json" {
		enc := json.NewEncoder(a.out)
		enc.SetIndent("", "  ")
		if findings == nil {
			findings = []status.Finding{}
		}
		if err := enc.Encode(findings); err != nil {
			return err
		}
	} else {
		a.printFindings(findings, scope)
	}
	for _, s := range skipped {
		fmt.Fprintln(a.err, a.pal.Dim("skipped "+s))
	}
	if len(findings) > 0 {
		return exitCodeError{code: exitProblems}
	}
	return nil
}

func (a *app) printFindings(findings []status.Finding, scope string) {
	if len(findings) == 0 {
		fmt.Fprintf(a.out, "%s Everything looks healthy in %s.\n", a.pal.Green("OK"), scope)
		return
	}
	showNS := a.opts.AllNamespaces
	groups := map[string][]status.Finding{}
	var kinds []string
	for _, f := range findings {
		if _, ok := groups[f.Kind]; !ok {
			kinds = append(kinds, f.Kind)
		}
		groups[f.Kind] = append(groups[f.Kind], f)
	}
	order := map[string]int{"Node": 0, "Deployment": 1, "StatefulSet": 2, "DaemonSet": 3, "Pod": 4, "Job": 5, "PersistentVolumeClaim": 6}
	sort.SliceStable(kinds, func(i, j int) bool { return order[kinds[i]] < order[kinds[j]] })

	crit, warn := 0, 0
	for _, k := range kinds {
		fmt.Fprintln(a.out, a.pal.Bold(k+"s"))
		header := []string{"NAME", "STATUS", "DETAIL"}
		if showNS && k != "Node" {
			header = append([]string{"NAMESPACE"}, header...)
		}
		var rows [][]cell
		for _, f := range groups[k] {
			st := plain(f.Status)
			if f.Severity == status.Critical {
				st.style = a.pal.Red
				crit++
			} else {
				st.style = a.pal.Yellow
				warn++
			}
			r := []cell{plain(f.Name), st, plain(f.Detail)}
			if showNS && k != "Node" {
				r = append([]cell{plain(f.Namespace)}, r...)
			}
			rows = append(rows, r)
		}
		table(a.out, header, rows)
		fmt.Fprintln(a.out)
	}
	fmt.Fprintf(a.out, "%d critical, %d warning in %s. Next: kubekaze why <kind>/<name>\n", crit, warn, scope)
}
