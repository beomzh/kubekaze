package cli

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/beomzh/kubekaze/internal/kube"
	"github.com/beomzh/kubekaze/internal/kubeconfig"
	"github.com/beomzh/kubekaze/internal/term"
	"github.com/beomzh/kubekaze/internal/ui"
)

func (a *app) newNsCmd() *cobra.Command {
	var current, list, noSummary bool
	cmd := &cobra.Command{
		Use:     "ns [name|-]",
		Aliases: []string{"project", "projects"},
		Short:   "List or switch the namespace (projects on OpenShift)",
		Long: `List or switch the namespace of the current context.
On OpenShift this lists and switches projects.

  kubekaze ns             pick interactively (or list when not a terminal)
  kubekaze ns payments    switch to a namespace
  kubekaze ns -           switch to the previous namespace
  kubekaze ns -c          print the current namespace
  kubekaze ns --list      list with pod count, unhealthy pods and recent Warning events
  kubekaze ns --list -o json   the same as JSON, for scripts

The picker shows the same summary next to each name (filtering still matches
the name). It reads pods and events cluster-wide once; when you are not allowed
to, it says so and shows plain names. --no-summary skips those calls.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			m := kubeconfig.New(a.opts.Kubeconfig, kubeconfig.DefaultStatePath())
			ctxName, cur, err := m.CurrentNamespace()
			if err != nil {
				return err
			}
			if current {
				fmt.Fprintln(a.out, term.SafeText(cur))
				return nil
			}
			target := ""
			if len(args) == 1 {
				target = args[0]
				if target != "-" {
					a.warnIfMissing(cmd, target)
				}
			} else {
				names, noun, err := a.opts.ListNamespaces(cmd.Context())
				if err != nil {
					return err
				}
				interactive := term.IsTerminal(a.in) && term.IsTerminal(a.out) && !list && a.output != "json"
				var sums nsSummaries
				if (list || a.output == "json" || interactive) && !noSummary {
					cs, cerr := a.clientset()
					if cerr != nil {
						return cerr
					}
					sums = summarizeNamespaces(cmd.Context(), cs, names, cur, time.Now())
				} else {
					sums = plainSummaries(names, cur)
				}
				if !interactive {
					return a.printNsList(sums, noun)
				}
				for _, n := range sums.Notes {
					fmt.Fprintln(a.err, a.pal.Yellow("note:"), n)
				}
				labels := names
				if sums.known() {
					labels = sums.pickLabels()
				}
				picked, err := ui.PickLabeled(names, labels, noun)
				if err != nil {
					return err
				}
				target = picked
			}
			ctxNow, nsNow, err := m.UseNamespace(target)
			if err != nil {
				return err
			}
			fmt.Fprintf(a.out, "Context %s: namespace set to %s\n",
				a.pal.Bold(term.SafeText(ctxNow)), a.pal.Bold(term.SafeText(nsNow)))
			_ = ctxName
			if kube.IsProduction(ctxNow) {
				fmt.Fprintln(a.err, a.pal.Red("warning:"), "this looks like a production context")
			}
			return nil
		},
	}
	cmd.Flags().BoolVarP(&current, "current", "c", false, "print the current namespace")
	cmd.Flags().BoolVarP(&list, "list", "l", false, "list non-interactively with pods, unhealthy pods and recent Warning events")
	cmd.Flags().BoolVar(&noSummary, "no-summary", false, "do not read pods and events (plain names only)")
	return cmd
}

// warnIfMissing checks the namespace exists when the API allows it and only
// warns, since users may lack permission to read namespaces.
func (a *app) warnIfMissing(cmd *cobra.Command, ns string) {
	cs, err := a.opts.Clientset()
	if err != nil {
		return
	}
	if _, err := cs.CoreV1().Namespaces().Get(cmd.Context(), ns, metav1.GetOptions{}); apierrors.IsNotFound(err) {
		fmt.Fprintln(a.err, a.pal.Yellow("warning:"), fmt.Sprintf("namespace %q was not found on the cluster", term.SafeText(ns)))
	}
}
