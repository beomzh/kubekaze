package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/spf13/cobra"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/beomzh/kubekaze/internal/changes"
	"github.com/beomzh/kubekaze/internal/events"
)

func (a *app) newChangesCmd() *cobra.Command {
	since := "1h"
	cmd := &cobra.Command{
		Use:   "changes",
		Short: "Show what changed recently: rollouts, config, scaling, node events",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, args []string) error { return a.runChanges(cmd.Context(), since) },
	}
	cmd.Flags().StringVar(&since, "since", since, "how far back to look (30m, 2h, 1d)")
	return cmd
}

// loadChanges lists what changes.Find needs. Kinds that cannot be listed are
// returned in skipped.
func loadChanges(ctx context.Context, cs kubernetes.Interface, ns string) (changes.Input, []string) {
	var in changes.Input
	var skipped []string
	// The lists run in parallel: each fills its own field of in.
	var wg sync.WaitGroup
	var mu sync.Mutex
	run := func(what string, fn listFn) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := fn()
			if err == nil {
				return
			}
			mu.Lock()
			defer mu.Unlock()
			if apierrors.IsForbidden(err) {
				skipped = append(skipped, what+" (forbidden)")
				return
			}
			skipped = append(skipped, fmt.Sprintf("%s (%v)", what, err))
		}()
	}
	lo := metav1.ListOptions{}
	run("deployments", func() error {
		l, err := cs.AppsV1().Deployments(ns).List(ctx, lo)
		if err == nil {
			in.Deployments = l.Items
		}
		return err
	})
	run("replicasets", func() error {
		l, err := cs.AppsV1().ReplicaSets(ns).List(ctx, lo)
		if err == nil {
			in.ReplicaSets = l.Items
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
	run("controllerrevisions", func() error {
		l, err := cs.AppsV1().ControllerRevisions(ns).List(ctx, lo)
		if err == nil {
			in.Revisions = l.Items
		}
		return err
	})
	run("configmaps", func() error {
		l, err := cs.CoreV1().ConfigMaps(ns).List(ctx, lo)
		if err == nil {
			in.ConfigMaps = l.Items
		}
		return err
	})
	run("secrets", func() error {
		l, err := cs.CoreV1().Secrets(ns).List(ctx, lo)
		if err == nil {
			in.Secrets = l.Items
		}
		return err
	})
	run("services", func() error {
		l, err := cs.CoreV1().Services(ns).List(ctx, lo)
		if err == nil {
			in.Services = l.Items
		}
		return err
	})
	run("ingresses", func() error {
		l, err := cs.NetworkingV1().Ingresses(ns).List(ctx, lo)
		if err == nil {
			in.Ingresses = l.Items
		}
		return err
	})
	// Only the two reasons that matter, filtered by the API server: a busy
	// cluster can hold a hundred thousand events.
	run("events", func() error {
		var all []events.Row
		for _, reason := range []string{"ScalingReplicaSet", "SuccessfulRescale"} {
			rows, _, err := fetchEvents(ctx, cs, []string{ns}, "reason="+reason)
			if err != nil {
				return err
			}
			all = append(all, rows...)
		}
		in.Events = events.Collapse(all)
		return nil
	})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for _, rows := range loadNodeEvents(ctx, cs, "") {
			in.NodeEvents = append(in.NodeEvents, events.Collapse(rows)...)
		}
	}()
	wg.Wait()
	sort.Strings(skipped)
	return in, skipped
}

func (a *app) runChanges(ctx context.Context, since string) error {
	d, err := events.ParseSince(since)
	if err != nil || d <= 0 {
		return fmt.Errorf("invalid --since %q (examples: 30m, 2h, 1d)", since)
	}
	cs, err := a.clientset()
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
	now := time.Now()
	in, skipped := loadChanges(ctx, cs, ns)
	in.Since = now.Add(-d)
	found := changes.Find(in)

	if a.output == "json" {
		enc := json.NewEncoder(a.out)
		enc.SetIndent("", "  ")
		if found == nil {
			found = []changes.Change{}
		}
		if err := enc.Encode(found); err != nil {
			return err
		}
	} else {
		a.printChanges(found, now)
		if len(found) == 0 {
			fmt.Fprintf(a.out, "No changes found in %s in the last %s. Try a longer --since.\n", scope, since)
		}
		fmt.Fprintln(a.err, a.pal.Dim(fmt.Sprintf("%d change(s) in %s in the last %s. Not visible to the API: deleted objects and previous values.", len(found), scope, since)))
	}
	for _, s := range skipped {
		fmt.Fprintln(a.err, a.pal.Dim("skipped "+s))
	}
	return nil
}

func (a *app) printChanges(found []changes.Change, now time.Time) {
	if len(found) == 0 {
		return
	}
	showNS := a.opts.AllNamespaces
	header := []string{"TIME", "AGO", "KIND", "NAME", "CHANGE", "BY"}
	if showNS {
		header = append([]string{"NAMESPACE"}, header...)
	}
	rows := make([][]cell, 0, len(found))
	for _, c := range found {
		what := c.Action
		if c.Detail != "" {
			what += ": " + c.Detail
		}
		r := []cell{plain(c.Time.Local().Format("01-02 15:04")), plain(events.Age(now.Sub(c.Time))), plain(c.Kind), plain(c.Name), plain(what), plain(c.By)}
		if showNS {
			ns := c.Namespace
			if ns == "" {
				ns = "-"
			}
			r = append([]cell{plain(ns)}, r...)
		}
		rows = append(rows, r)
	}
	table(a.out, header, rows)
}
