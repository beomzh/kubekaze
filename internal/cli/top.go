package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/beomzh/kubekaze/internal/term"
	"github.com/beomzh/kubekaze/internal/ui"
	"github.com/beomzh/kubekaze/internal/usage"
)

const metricsBase = "/apis/metrics.k8s.io/v1beta1/"

type topFlags struct {
	limit       int
	sortKey     string
	byNamespace bool
	watch       bool
	interval    time.Duration
}

func (a *app) newTopCmd() *cobra.Command {
	var f topFlags
	cmd := &cobra.Command{
		Use:       "top [ns]",
		Short:     "Show CPU and memory usage of nodes, pods and namespaces",
		Args:      cobra.MaximumNArgs(1),
		ValidArgs: []string{"ns"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
				if args[0] != "ns" {
					return fmt.Errorf("unknown argument %q (want ns)", args[0])
				}
				f.byNamespace = true
			}
			if f.sortKey != usage.SortCPU && f.sortKey != usage.SortMemory {
				return fmt.Errorf("invalid --sort %q (want cpu or memory)", f.sortKey)
			}
			if f.interval < time.Second {
				return fmt.Errorf("--interval must be at least 1s")
			}
			return a.runTop(cmd.Context(), f)
		},
	}
	fl := cmd.Flags()
	fl.IntVar(&f.limit, "limit", 10, "number of pods (or namespaces) to show; 0 = all")
	fl.StringVar(&f.sortKey, "sort", usage.SortCPU, "sort pods and namespaces by cpu|memory")
	fl.BoolVar(&f.byNamespace, "by-namespace", false, "sum usage per namespace (same as \"top ns\")")
	fl.BoolVarP(&f.watch, "watch", "w", false, "redraw every --interval until Ctrl-C")
	fl.DurationVar(&f.interval, "interval", 5*time.Second, "refresh interval for --watch")
	return cmd
}

// fetchMetrics fetches a metrics.k8s.io path; tests replace it.
func (a *app) fetchMetrics(ctx context.Context, path string) ([]byte, error) {
	cs, err := a.clientset()
	if err != nil {
		return nil, err
	}
	return cs.CoreV1().RESTClient().Get().AbsPath(metricsBase + path).DoRaw(ctx)
}

// explainMetricsErr turns an API failure into one line saying what to do.
func explainMetricsErr(err error) error {
	switch {
	case apierrors.IsNotFound(err), apierrors.IsServiceUnavailable(err):
		return errors.New("metrics API (metrics.k8s.io) is not available: install metrics-server " +
			"(https://github.com/kubernetes-sigs/metrics-server) and wait until it is Ready")
	case apierrors.IsForbidden(err), apierrors.IsUnauthorized(err):
		return errors.New("not allowed to read the metrics API: grant get/list on nodes and pods in group metrics.k8s.io (e.g. the built-in \"view\" role covers pods)")
	}
	return fmt.Errorf("reading metrics: %w", err)
}

// snapshot takes one measurement. Parts that are forbidden are skipped with a
// note; if nothing could be read the explained error is returned.
func (a *app) snapshot(ctx context.Context, f topFlags) (usage.Snapshot, []string, error) {
	var snap usage.Snapshot
	var notes []string
	cs, err := a.clientset()
	if err != nil {
		return snap, nil, err
	}
	ns, err := a.opts.NamespaceOrAll()
	if err != nil {
		return snap, nil, err
	}
	if f.byNamespace {
		ns = metav1.NamespaceAll
	}
	var firstErr error
	fail := func(what string, err error) {
		if firstErr == nil {
			firstErr = explainMetricsErr(err)
		}
		if apierrors.IsForbidden(err) {
			notes = append(notes, what+" (forbidden)")
		} else {
			notes = append(notes, what)
		}
	}

	if !f.byNamespace {
		raw, err := a.fetchMetrics(ctx, "nodes")
		if err == nil {
			var nodes = cs.CoreV1().Nodes()
			l, lerr := nodes.List(ctx, metav1.ListOptions{})
			if lerr != nil {
				notes = append(notes, "node capacity (cannot list nodes)")
			}
			snap.Nodes, err = usage.Nodes(raw, itemsOrNil(l, lerr))
		}
		if err != nil {
			fail("node usage", err)
		}
	}

	path := "pods"
	if ns != "" {
		path = "namespaces/" + ns + "/pods"
	}
	raw, err := a.fetchMetrics(ctx, path)
	if err == nil {
		l, lerr := cs.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{})
		if lerr != nil {
			notes = append(notes, "requests/limits (cannot list pods)")
		}
		var pods []usage.Pod
		pods, err = usage.Pods(raw, podItems(l, lerr))
		if err == nil {
			usage.SortPods(pods, f.sortKey)
			if f.byNamespace {
				snap.Namespaces = usage.Limit(usage.ByNamespace(pods, f.sortKey), f.limit)
			} else {
				snap.Pods = usage.Limit(pods, f.limit)
			}
		}
	}
	if err != nil {
		fail("pod usage", err)
	}

	if firstErr != nil && len(snap.Nodes) == 0 && len(snap.Pods) == 0 && len(snap.Namespaces) == 0 {
		return snap, nil, firstErr
	}
	return snap, notes, nil
}

func (a *app) runTop(ctx context.Context, f topFlags) error {
	tty := term.IsTerminal(a.out)
	if f.watch && tty {
		if of, ok := a.out.(*os.File); ok {
			defer term.EnableVT(of)()
		}
	}
	first := true
	for {
		snap, notes, err := a.snapshot(ctx, f)
		if err != nil {
			if ctx.Err() != nil {
				return ui.ErrCancelled
			}
			if first || !f.watch {
				return err
			}
			notes = []string{err.Error()}
		}
		first = false
		var buf bytes.Buffer
		if a.output == "json" {
			enc := json.NewEncoder(&buf)
			if !f.watch {
				enc.SetIndent("", "  ")
			}
			if err := enc.Encode(snap); err != nil {
				return err
			}
		} else {
			a.printTop(&buf, snap, f)
		}
		if f.watch && tty && a.output != "json" {
			io.WriteString(a.out, "\x1b[H\x1b[2J")
		} else if f.watch && a.output != "json" {
			io.WriteString(a.out, "---- "+time.Now().Format("15:04:05")+" ----\n")
		}
		_, _ = a.out.Write(buf.Bytes())
		for _, n := range notes {
			fmt.Fprintln(a.err, a.pal.Dim("skipped "+n))
		}
		if !f.watch {
			return nil
		}
		select {
		case <-ctx.Done():
			return ui.ErrCancelled
		case <-time.After(f.interval):
		}
	}
}

func (a *app) pctCell(v float64) cell {
	c := plain(fmt.Sprintf("%.0f%%", v))
	if v > usage.HighPercent {
		c.style = a.pal.Red
	}
	return c
}

// limCell shows usage as percent of the limit, or "-" when none is set.
func (a *app) limCell(v float64, limit int64) cell {
	if limit <= 0 {
		return cell{text: "-", style: a.pal.Dim}
	}
	return a.pctCell(v)
}

func (a *app) printTop(w io.Writer, s usage.Snapshot, f topFlags) {
	if len(s.Nodes) > 0 {
		fmt.Fprintln(w, a.pal.Bold("Nodes"))
		var rows [][]cell
		for _, n := range s.Nodes {
			rows = append(rows, []cell{plain(n.Name), plain(usage.FormatCPU(n.CPUMilli)), a.pctCell(n.CPUPercent),
				plain(usage.FormatMem(n.MemBytes)), a.pctCell(n.MemPercent)})
		}
		table(w, []string{"NAME", "CPU", "CPU%", "MEMORY", "MEM%"}, rows)
		fmt.Fprintln(w)
	}
	if len(s.Namespaces) > 0 {
		fmt.Fprintln(w, a.pal.Bold("Namespaces (by "+f.sortKey+")"))
		var rows [][]cell
		for _, n := range s.Namespaces {
			rows = append(rows, []cell{plain(n.Name), plain(fmt.Sprint(n.Pods)), plain(usage.FormatCPU(n.CPUMilli)), plain(usage.FormatMem(n.MemBytes))})
		}
		table(w, []string{"NAMESPACE", "PODS", "CPU", "MEMORY"}, rows)
		return
	}
	if len(s.Pods) > 0 {
		title := "Pods (top " + f.sortKey + ")"
		if a.opts.AllNamespaces {
			title += ", all namespaces"
		}
		fmt.Fprintln(w, a.pal.Bold(title))
		header := []string{"NAME", "CPU", "CPU/LIMIT", "MEMORY", "MEM/LIMIT"}
		if a.opts.AllNamespaces {
			header = append([]string{"NAMESPACE"}, header...)
		}
		var rows [][]cell
		for _, p := range s.Pods {
			r := []cell{plain(p.Name), plain(usage.FormatCPU(p.CPUMilli)), a.limCell(p.CPULimitPct, p.CPULimit),
				plain(usage.FormatMem(p.MemBytes)), a.limCell(p.MemLimitPct, p.MemLimit)}
			if a.opts.AllNamespaces {
				r = append([]cell{plain(p.Namespace)}, r...)
			}
			rows = append(rows, r)
		}
		table(w, header, rows)
		fmt.Fprintf(w, "\nRed = above %d%% of the limit (throttling or OOM risk); \"-\" = no limit set.\n", usage.HighPercent)
	} else if len(s.Nodes) == 0 {
		fmt.Fprintln(w, "No usage data.")
	}
}

func itemsOrNil(l *corev1.NodeList, err error) []corev1.Node {
	if err != nil || l == nil {
		return nil
	}
	return l.Items
}

func podItems(l *corev1.PodList, err error) []corev1.Pod {
	if err != nil || l == nil {
		return nil
	}
	return l.Items
}
