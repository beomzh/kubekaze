package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/beomzh/kubekaze/internal/kube"
	"github.com/beomzh/kubekaze/internal/kubeconfig"
	"github.com/beomzh/kubekaze/internal/term"
	"github.com/beomzh/kubekaze/internal/ui"
)

func (a *app) newCtxCmd() *cobra.Command {
	var current bool
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
				labels := make([]string, len(names))
				for i, n := range names {
					labels[i] = n
					if n == cur {
						labels[i] = n + "  (current)"
					}
					if kube.IsProduction(n) {
						labels[i] += "  [prod]"
					}
				}
				picked, err := ui.Pick(labels, "context")
				if err != nil {
					return err
				}
				target = strings.Fields(picked)[0]
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
			return nil
		},
	}
	cmd.Flags().BoolVarP(&current, "current", "c", false, "print the current context")
	return cmd
}
