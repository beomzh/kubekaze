package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/beomzh/kubekaze/internal/events"
	"github.com/beomzh/kubekaze/internal/status"
	"github.com/beomzh/kubekaze/internal/term"
)

// warningWindow is how far back a Warning event counts as "recent".
const warningWindow = time.Hour

// nsSummary is the compact health line shown next to a namespace.
// Pods and Warnings are -1 when they could not be determined.
type nsSummary struct {
	Name      string `json:"name"`
	Current   bool   `json:"current"`
	Pods      int    `json:"pods"`
	Unhealthy int    `json:"unhealthyPods"`
	Warnings  int    `json:"recentWarnings"`
}

// nsSummaries holds the per-namespace summaries plus notes for whatever was
// not readable (for example RBAC forbids listing pods cluster-wide).
type nsSummaries struct {
	Items []nsSummary
	Notes []string
}

// summarizeNamespaces lists pods and events once, cluster-wide, and folds them
// into one summary per name. A forbidden (or otherwise failing) list is not an
// error: that part is marked unknown (-1) and a note says why.
func summarizeNamespaces(ctx context.Context, cs kubernetes.Interface, names []string, current string, now time.Time) nsSummaries {
	res := nsSummaries{Items: make([]nsSummary, len(names))}
	idx := make(map[string]int, len(names))
	for i, n := range names {
		res.Items[i] = nsSummary{Name: n, Current: n == current}
		idx[n] = i
	}

	pods, err := cs.CoreV1().Pods(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		res.Notes = append(res.Notes, "pods not shown: "+listFailure("pods", err))
		for i := range res.Items {
			res.Items[i].Pods, res.Items[i].Unhealthy = -1, -1
		}
	} else {
		for _, p := range pods.Items {
			if i, ok := idx[p.Namespace]; ok {
				res.Items[i].Pods++
			}
		}
		// Reuse the status rules (startup grace, Completed pods); restart
		// counts alone do not make a pod unhealthy here.
		for _, f := range status.Analyze(status.Input{Pods: pods.Items, Now: now, RestartThreshold: math.MaxInt32}) {
			if f.Kind == "Pod" {
				if i, ok := idx[f.Namespace]; ok {
					res.Items[i].Unhealthy++
				}
			}
		}
	}

	evs, err := cs.CoreV1().Events(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		res.Notes = append(res.Notes, "events not shown: "+listFailure("events", err))
		for i := range res.Items {
			res.Items[i].Warnings = -1
		}
	} else {
		rows := make([]events.Row, 0, len(evs.Items))
		for _, e := range evs.Items {
			rows = append(rows, events.FromCore(e))
		}
		rows = events.Filter{Types: []string{"Warning"}, Since: warningWindow, Now: now}.Apply(rows)
		for _, r := range events.Collapse(rows) {
			if i, ok := idx[r.Namespace]; ok {
				res.Items[i].Warnings++
			}
		}
	}
	return res
}

func listFailure(what string, err error) string {
	if apierrors.IsForbidden(err) {
		return "you are not allowed to list " + what + " cluster-wide"
	}
	return err.Error()
}

// known reports whether any summary data is available.
func (s nsSummaries) known() bool {
	for _, it := range s.Items {
		if it.Pods >= 0 || it.Warnings >= 0 {
			return true
		}
	}
	return false
}

// text is the compact summary, e.g. "12 pods, 2 unhealthy, 5 warnings".
func (s nsSummary) text() string {
	var parts []string
	if s.Pods >= 0 {
		parts = append(parts, fmt.Sprintf("%d pods", s.Pods), fmt.Sprintf("%d unhealthy", s.Unhealthy))
	}
	if s.Warnings >= 0 {
		parts = append(parts, fmt.Sprintf("%d warnings", s.Warnings))
	}
	return strings.Join(parts, ", ")
}

// pickLabels returns one line per namespace for the interactive picker. The
// name comes first, so fuzzy filtering by name still works.
func (s nsSummaries) pickLabels() []string {
	width := 0
	for _, it := range s.Items {
		if n := len([]rune(term.SafeText(it.Name))); n > width {
			width = n
		}
	}
	out := make([]string, len(s.Items))
	for i, it := range s.Items {
		name := term.SafeText(it.Name)
		out[i] = name + strings.Repeat(" ", width-len([]rune(name))) + "  " + it.text()
	}
	return out
}

// printList writes the non-interactive summary (text table or JSON).
func (a *app) printNsList(s nsSummaries, noun string) error {
	if a.output == "json" {
		enc := json.NewEncoder(a.out)
		enc.SetIndent("", "  ")
		items := s.Items
		if items == nil {
			items = []nsSummary{}
		}
		return enc.Encode(map[string]any{"kind": noun, "items": items, "notes": notes(s.Notes)})
	}
	labels := s.pickLabels()
	for i, it := range s.Items {
		mark := "  "
		if it.Current {
			mark = "* "
		}
		line := labels[i]
		if !s.known() {
			line = term.SafeText(it.Name)
		}
		fmt.Fprintln(a.out, mark+strings.TrimRight(line, " "))
	}
	for _, n := range s.Notes {
		fmt.Fprintln(a.err, a.pal.Yellow("note:"), n)
	}
	return nil
}

func notes(n []string) []string {
	if n == nil {
		return []string{}
	}
	return n
}

// plainSummaries is a name-only list: every count is unknown.
func plainSummaries(names []string, current string) nsSummaries {
	res := nsSummaries{Items: make([]nsSummary, len(names))}
	for i, n := range names {
		res.Items[i] = nsSummary{Name: n, Current: n == current, Pods: -1, Unhealthy: -1, Warnings: -1}
	}
	return res
}
