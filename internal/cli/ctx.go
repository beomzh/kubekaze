package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/beomzh/kubekaze/internal/kube"
	"github.com/beomzh/kubekaze/internal/kubeconfig"
	"github.com/beomzh/kubekaze/internal/term"
	"github.com/beomzh/kubekaze/internal/ui"
)

func (a *app) newCtxCmd() *cobra.Command {
	var current, noCheck, all bool
	cmd := &cobra.Command{
		Use:   "ctx [name|-]",
		Short: "List or switch the kubeconfig context",
		Long: `List or switch the kubeconfig context.

  kubekaze ctx            pick interactively (or list when not a terminal)
  kubekaze ctx prod-eks   switch to a context
  kubekaze ctx -          switch to the previous context
  kubekaze ctx -c         print the current context`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			m := kubeconfig.New(a.opts.Kubeconfig, kubeconfig.DefaultStatePath())
			names, cur, err := m.Contexts()
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
			} else if term.IsTerminal(a.in) && term.IsTerminal(a.out) {
				picked, err := a.pickContext(m, names, cur, all)
				if err != nil {
					return err
				}
				target = picked
			} else {
				for _, n := range names {
					mark := "  "
					if n == cur {
						mark = "* "
					}
					fmt.Fprintln(a.out, mark+term.SafeText(n))
				}
				return nil
			}
			now, err := m.UseContext(target)
			if err != nil {
				return err
			}
			fmt.Fprintf(a.out, "Switched to context %s\n", a.pal.Bold(term.SafeText(now)))
			if kube.IsProduction(now) {
				fmt.Fprintln(a.err, a.pal.Red("warning:"), "this looks like a production context")
			}
			if !noCheck {
				a.checkContext(cmd.Context())
			}
			return nil
		},
	}
	cmd.Flags().BoolVarP(&current, "current", "c", false, "print the current context")
	cmd.Flags().BoolVar(&all, "all", false, "in the picker, list every context instead of grouping by cluster and user")
	cmd.Flags().BoolVar(&noCheck, "no-check", false, "do not test the connection and login after switching")
	return cmd
}

// checkContext tells the user right after a switch when the new context does
// not work (expired login, VPN down), instead of at the first failing command.
func (a *app) checkContext(ctx context.Context) {
	o := kube.Options{Kubeconfig: a.opts.Kubeconfig}
	cfg, err := o.REST()
	if err == nil {
		err = kube.Check(ctx, cfg)
	}
	if err != nil {
		fmt.Fprintln(a.err, a.pal.Yellow("warning:"), term.SafeText(err.Error()))
		return
	}
	fmt.Fprintln(a.err, a.pal.Dim("API server reachable, login accepted."))
}

// pickContext shows the interactive picker. When several contexts share a
// cluster and user (what "oc project" creates, one per namespace) they are
// collapsed into one row, because the namespace is chosen with "kubekaze ns".
func (a *app) pickContext(m *kubeconfig.Manager, names []string, cur string, all bool) (string, error) {
	infos, _, err := m.ContextInfos()
	if err != nil {
		return "", err
	}
	groups := kubeconfig.GroupContexts(infos, cur)
	if all || len(groups) == len(infos) {
		labels := make([]string, len(names))
		byLabel := map[string]string{}
		for i, n := range names {
			labels[i] = term.SafeText(n)
			if n == cur {
				labels[i] += "  (current)"
			}
			if kube.IsProduction(n) {
				labels[i] += "  [prod]"
			}
			byLabel[labels[i]] = n
		}
		picked, err := ui.Pick(labels, "context")
		if err != nil {
			return "", err
		}
		return byLabel[picked], nil
	}
	uw, cw := 0, 0
	for _, g := range groups {
		uw, cw = max(uw, len(g.User)), max(cw, len(g.Cluster))
	}
	labels := make([]string, len(groups))
	byLabel := map[string]string{}
	for i, g := range groups {
		l := fmt.Sprintf("%-*s  %-*s", uw, term.SafeText(g.User), cw, term.SafeText(g.Cluster))
		if g.Count > 1 {
			l += fmt.Sprintf("  (%d namespaces)", g.Count)
		} else if g.Rep.Namespace != "" {
			l += "  ns=" + term.SafeText(g.Rep.Namespace)
		}
		if g.Current {
			l += "  (current)"
		}
		if kube.IsProduction(g.Rep.Name) || kube.IsProduction(g.Cluster) {
			l += "  [prod]"
		}
		labels[i] = l
		byLabel[l] = g.Rep.Name
	}
	picked, err := ui.Pick(labels, "user / cluster  (use ns to change namespace; --all lists every context)")
	if err != nil {
		return "", err
	}
	return byLabel[picked], nil
}
