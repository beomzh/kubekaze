package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/beomzh/kubekaze/internal/kube"
	"github.com/beomzh/kubekaze/internal/kubeconfig"
	"github.com/beomzh/kubekaze/internal/term"
	"github.com/beomzh/kubekaze/internal/ui"
)

func (a *app) newNsCmd() *cobra.Command {
	var current bool
	cmd := &cobra.Command{
		Use:     "ns [name|-]",
		Aliases: []string{"project", "projects"},
		Short:   "List or switch the namespace (projects on OpenShift)",
		Long: `List or switch the namespace of the current context.
On OpenShift this lists and switches projects.

  kubekaze ns             pick interactively (or list when not a terminal)
  kubekaze ns payments    switch to a namespace
  kubekaze ns -           switch to the previous namespace
  kubekaze ns -c          print the current namespace`,
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
				if !(term.IsTerminal(a.in) && term.IsTerminal(a.out)) {
					for _, n := range names {
						mark := "  "
						if n == cur {
							mark = "* "
						}
						fmt.Fprintln(a.out, mark+term.SafeText(n))
					}
					return nil
				}
				picked, err := ui.Pick(names, noun)
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
