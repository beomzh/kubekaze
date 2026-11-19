package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"

	"github.com/beomzh/kubekaze/internal/events"
	"github.com/beomzh/kubekaze/internal/term"
)

type eventsFlags struct {
	sortKey    string
	reverse    bool
	types      []string
	reasons    []string
	since      string
	forObj     string
	summary    bool
	noCollapse bool
	watch      bool
}

func (a *app) newEventsCmd() *cobra.Command {
	var f eventsFlags
	cmd := &cobra.Command{
		Use:   "events",
		Short: "Show events sorted by time (or count, type, ...) across namespaces",
		Long: `Show events the way you actually need them.

By default events are sorted by last-seen time with the newest at the bottom,
repeated events are collapsed into one line with a count, and several
namespaces can be shown at once:

  kubekaze events -A --type Warning
  kubekaze events -n payments,orders --since 30m
  kubekaze events -A --summary          one line per namespace
  kubekaze events --for pod/api-7d9f
  kubekaze events -A --sort count --reverse

Kubernetes keeps events for about an hour by default.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error { return a.runEvents(cmd.Context(), f) },
	}
	fl := cmd.Flags()
	fl.StringVar(&f.sortKey, "sort", "time", "sort by: "+strings.Join(events.SortKeys, "|"))
	fl.BoolVar(&f.reverse, "reverse", false, "reverse the sort order")
	fl.StringSliceVar(&f.types, "type", nil, "only these types, e.g. Warning")
	fl.StringSliceVar(&f.reasons, "reason", nil, "only these reasons, e.g. BackOff,FailedScheduling")
	fl.StringVar(&f.since, "since", "", "only events seen within this duration (30m, 2h, 1d)")
	fl.StringVar(&f.forObj, "for", "", "only events of an object: kind/name (e.g. pod/api) or name")
	fl.BoolVar(&f.summary, "summary", false, "one line per namespace instead of events")
	fl.BoolVar(&f.noCollapse, "no-collapse", false, "do not fold repeated events")
	fl.BoolVarP(&f.watch, "watch", "w", false, "keep watching for new events")
	return cmd
}

var kindAliases = map[string]string{
	"po": "pod", "pods": "pod", "deploy": "deployment", "deployments": "deployment",
	"rs": "replicaset", "sts": "statefulset", "ds": "daemonset", "no": "node", "nodes": "node",
	"svc": "service", "pvc": "persistentvolumeclaim", "cj": "cronjob", "jobs": "job",
	"hpa": "horizontalpodautoscaler", "ing": "ingress",
}

func parseFor(s string) (kind, name string) {
	if s == "" {
		return "", ""
	}
	k, n, ok := strings.Cut(s, "/")
	if !ok {
		return "", s
	}
	k = strings.ToLower(k)
	if alias, ok := kindAliases[k]; ok {
		k = alias
	}
	return k, n
}

func splitNamespaces(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// fetchEvents lists events from the given namespaces ("" = all).
func fetchEvents(ctx context.Context, cs kubernetes.Interface, namespaces []string, fieldSelector string) ([]events.Row, string, error) {
	var rows []events.Row
	rv := ""
	for _, ns := range namespaces {
		opts := metav1.ListOptions{FieldSelector: fieldSelector, Limit: 500}
		for {
			list, err := cs.CoreV1().Events(ns).List(ctx, opts)
			if err != nil {
				return nil, "", fmt.Errorf("listing events: %w", err)
			}
			for _, e := range list.Items {
				rows = append(rows, events.FromCore(e))
			}
			rv = list.ResourceVersion
			if list.Continue == "" {
				break
			}
			opts.Continue = list.Continue
		}
	}
	return rows, rv, nil
}

func (a *app) runEvents(ctx context.Context, f eventsFlags) error {
	valid := false
	for _, k := range events.SortKeys {
		if f.sortKey == k {
			valid = true
		}
	}
	if !valid {
		return fmt.Errorf("invalid --sort %q (want one of: %s)", f.sortKey, strings.Join(events.SortKeys, ", "))
	}
	var since time.Duration
	if f.since != "" {
		d, err := events.ParseSince(f.since)
		if err != nil || d <= 0 {
			return fmt.Errorf("invalid --since %q (examples: 30m, 2h, 1d)", f.since)
		}
		since = d
	}

	cs, err := a.opts.Clientset()
	if err != nil {
		return err
	}
	var namespaces []string
	switch {
	case a.opts.AllNamespaces:
		namespaces = []string{metav1.NamespaceAll}
	case strings.Contains(a.opts.Namespace, ","):
		namespaces = splitNamespaces(a.opts.Namespace)
	default:
		ns, err := a.opts.ResolvedNamespace()
		if err != nil {
			return err
		}
		namespaces = []string{ns}
	}
	showNS := len(namespaces) > 1 || namespaces[0] == metav1.NamespaceAll

	kind, name := parseFor(f.forObj)
	fieldSel := ""
	if name != "" {
		fieldSel = "involvedObject.name=" + name
	}
	filter := events.Filter{Types: f.types, Reasons: f.reasons, Since: since, Now: time.Now(), Kind: kind, Name: name}

	rows, rv, err := fetchEvents(ctx, cs, namespaces, fieldSel)
	if err != nil {
		return err
	}
	rows = filter.Apply(rows)
	if !f.noCollapse {
		rows = events.Collapse(rows)
	}

	if f.summary {
		return a.printSummary(rows)
	}
	events.Sort(rows, f.sortKey, f.reverse)

	if a.output == "json" {
		enc := json.NewEncoder(a.out)
		enc.SetIndent("", "  ")
		if rows == nil {
			rows = []events.Row{}
		}
		return enc.Encode(rows)
	}

	now := time.Now()
	a.printEvents(rows, showNS, now)
	if len(rows) == 0 {
		fmt.Fprintln(a.err, a.pal.Dim("No events found."))
	} else {
		oldest := rows[0].Last
		for _, r := range rows {
			if r.Last.Before(oldest) {
				oldest = r.Last
			}
		}
		fmt.Fprintln(a.err, a.pal.Dim(fmt.Sprintf("%d events, oldest seen %s ago. Kubernetes keeps events for about 1h by default.",
			len(rows), events.Age(now.Sub(oldest)))))
	}
	if f.watch {
		return a.watchEvents(ctx, cs, namespaces, fieldSel, rv, filter, showNS)
	}
	return nil
}

func (a *app) printEvents(rows []events.Row, showNS bool, now time.Time) {
	header := []string{"LAST SEEN", "TYPE", "REASON", "OBJECT", "MESSAGE"}
	if showNS {
		header = append([]string{"NAMESPACE"}, header...)
	}
	out := make([][]cell, 0, len(rows))
	for _, r := range rows {
		out = append(out, a.eventCells(r, showNS, now))
	}
	table(a.out, header, out)
}

func (a *app) eventCells(r events.Row, showNS bool, now time.Time) []cell {
	typeCell := plain(r.Type)
	if r.Type == "Warning" {
		typeCell.style = a.pal.Yellow
	}
	msg := r.Message
	if r.Count > 1 {
		span := r.Last.Sub(r.First)
		if span >= time.Minute {
			msg += fmt.Sprintf("  (×%d over %s)", r.Count, events.Age(span))
		} else {
			msg += fmt.Sprintf("  (×%d)", r.Count)
		}
	}
	cells := []cell{plain(events.Age(now.Sub(r.Last))), typeCell, plain(r.Reason), plain(r.Object()), plain(msg)}
	if showNS {
		cells = append([]cell{plain(r.Namespace)}, cells...)
	}
	return cells
}

func (a *app) printSummary(rows []events.Row) error {
	sum := events.Summarize(rows)
	if a.output == "json" {
		enc := json.NewEncoder(a.out)
		enc.SetIndent("", "  ")
		if sum == nil {
			sum = []events.NamespaceSummary{}
		}
		return enc.Encode(sum)
	}
	now := time.Now()
	out := make([][]cell, 0, len(sum))
	for _, s := range sum {
		warn := plain(fmt.Sprint(s.Warnings))
		if s.Warnings > 0 {
			warn.style = a.pal.Yellow
		}
		top := strings.Join(s.TopReasons, ", ")
		out = append(out, []cell{plain(s.Namespace), plain(fmt.Sprint(s.Events)), warn, plain(events.Age(now.Sub(s.Last))), plain(top)})
	}
	table(a.out, []string{"NAMESPACE", "EVENTS", "WARNINGS", "LAST SEEN", "TOP WARNING REASONS"}, out)
	if len(sum) == 0 {
		fmt.Fprintln(a.err, a.pal.Dim("No events found."))
	}
	return nil
}

func (a *app) watchEvents(ctx context.Context, cs kubernetes.Interface, namespaces []string, fieldSel, rv string, filter events.Filter, showNS bool) error {
	if len(namespaces) != 1 {
		// Watching several namespaces needs one watch each; keep it simple for now.
		return fmt.Errorf("--watch supports one namespace or -A")
	}
	w, err := cs.CoreV1().Events(namespaces[0]).Watch(ctx, metav1.ListOptions{FieldSelector: fieldSel, ResourceVersion: rv})
	if err != nil {
		return fmt.Errorf("watching events: %w", err)
	}
	defer w.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case ev, ok := <-w.ResultChan():
			if !ok {
				return nil
			}
			if ev.Type != watch.Added && ev.Type != watch.Modified {
				continue
			}
			ce, ok := ev.Object.(*corev1.Event)
			if !ok {
				continue
			}
			filter.Now = time.Now()
			got := filter.Apply([]events.Row{events.FromCore(*ce)})
			sort.Slice(got, func(i, j int) bool { return got[i].Last.Before(got[j].Last) })
			for _, r := range got {
				cells := a.eventCells(r, showNS, time.Now())
				var parts []string
				for _, c := range cells {
					t := firstLine(c.text)
					if c.style != nil {
						t = c.style(t)
					}
					parts = append(parts, t)
				}
				fmt.Fprintln(a.out, strings.Join(parts, "  "))
			}
		}
	}
}

var _ = term.SafeText
