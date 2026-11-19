// Package cli wires the kubekaze commands.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"

	"github.com/spf13/cobra"

	"github.com/beomzh/kubekaze/internal/kube"
	"github.com/beomzh/kubekaze/internal/term"
	"github.com/beomzh/kubekaze/internal/ui"
	"github.com/beomzh/kubekaze/internal/version"
)

// app carries shared state for all commands.
type app struct {
	opts kube.Options
	in   io.Reader
	out  io.Writer
	err  io.Writer
	pal  term.Palette

	output      string
	interactive bool // stdin and stdout are terminals
}

// Execute runs the CLI and returns the process exit code.
func Execute(args []string, in io.Reader, out, errOut io.Writer) int {
	a := &app{in: in, out: out, err: errOut, pal: term.NewPalette(out), interactive: term.IsTerminal(in) && term.IsTerminal(out)}
	root := a.newRoot()
	root.SetArgs(args)
	root.SetOut(out)
	root.SetErr(errOut)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if err := root.ExecuteContext(ctx); err != nil {
		if errors.Is(err, ui.ErrCancelled) {
			return 130
		}
		var ec exitCodeError
		if errors.As(err, &ec) {
			return ec.code
		}
		fmt.Fprintln(errOut, a.pal.Red("error:"), term.SafeText(err.Error()))
		return 1
	}
	return 0
}

func (a *app) newRoot() *cobra.Command {
	root := &cobra.Command{
		Use:           "kubekaze",
		Short:         "Terminal-first Kubernetes companion: what is wrong, and why",
		Long:          "kubekaze finds out what is wrong in your cluster and why, in one command.\nIt reads your existing kubeconfig and is read-only unless a command says otherwise.",
		Version:       version.String(),
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if a.output != "text" && a.output != "json" {
				return fmt.Errorf("invalid --output %q (want text or json)", a.output)
			}
			return nil
		},
	}
	f := root.PersistentFlags()
	f.StringVar(&a.opts.Kubeconfig, "kubeconfig", "", "path to the kubeconfig file (default: $KUBECONFIG or ~/.kube/config)")
	f.StringVar(&a.opts.Context, "context", "", "kubeconfig context to use")
	f.StringVarP(&a.opts.Namespace, "namespace", "n", "", "namespace to use")
	f.StringVarP(&a.output, "output", "o", "text", "output format: text|json")
	f.BoolVarP(&a.opts.AllNamespaces, "all-namespaces", "A", false, "all namespaces")

	root.AddCommand(a.newCtxCmd(), a.newNsCmd(), a.newEventsCmd(), a.newRmCmd(), a.newShCmd(), a.newStatusCmd(), a.newWhyCmd(), a.newLogsCmd())
	return root
}
