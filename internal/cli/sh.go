package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	utilexec "k8s.io/client-go/util/exec"

	"github.com/beomzh/kubekaze/internal/kube"
	"github.com/beomzh/kubekaze/internal/shell"
	"github.com/beomzh/kubekaze/internal/target"
	"github.com/beomzh/kubekaze/internal/term"
	"github.com/beomzh/kubekaze/internal/ui"
)

// exitCodeError carries a remote command's exit status without an error message.
type exitCodeError struct{ code int }

func (e exitCodeError) Error() string { return fmt.Sprintf("exit status %d", e.code) }

type shFlags struct {
	container  string
	selector   string
	shell      string
	debug      bool
	debugImage string
	yes        bool
}

func (a *app) newShCmd() *cobra.Command {
	var f shFlags
	cmd := &cobra.Command{
		Use:     "sh [pod | deploy/name | sts/name | ...] [-- command...]",
		Aliases: []string{"exec", "rsh"},
		Short:   "Open a shell in a pod; finds bash/sh/ash for you",
		Long: `Open an interactive shell in a pod without guessing which shell the image has.

The target can be a pod name, a workload (deploy/api, sts/db, ds/agent, job/x)
or a label selector (-l). For a workload, a running and ready pod is chosen.
Shells are probed in order: bash, sh, ash, busybox sh. If the image has none
(distroless/scratch), kubekaze can add an ephemeral debug container instead.

  kubekaze sh api-7d9f
  kubekaze sh deploy/api -c sidecar
  kubekaze sh -l app=web
  kubekaze sh api-7d9f -- ls -l /app     run one command, no terminal needed

Ephemeral containers stay in the pod until it is deleted. The default debug image
is pinned by digest; override with --debug-image or KUBEKAZE_DEBUG_IMAGE.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			dash := cmd.ArgsLenAtDash()
			var ref string
			var remote []string
			switch {
			case dash < 0 && len(args) > 1:
				return fmt.Errorf("unexpected arguments %v; put the command after --", args[1:])
			case dash < 0 && len(args) == 1:
				ref = args[0]
			case dash == 1:
				ref, remote = args[0], args[1:]
			case dash == 0:
				remote = args
			default:
				return fmt.Errorf("give one target before --")
			}
			return a.runSh(cmd.Context(), ref, remote, f)
		},
	}
	fl := cmd.Flags()
	fl.StringVarP(&f.container, "container", "c", "", "container name")
	fl.StringVarP(&f.selector, "selector", "l", "", "label selector (instead of a pod name)")
	fl.StringVar(&f.shell, "shell", "", "use this shell instead of probing (e.g. /bin/zsh)")
	fl.BoolVar(&f.debug, "debug", false, "start an ephemeral debug container instead of using the container's shell")
	fl.StringVar(&f.debugImage, "debug-image", "", "image for the debug container (fixed tag or digest)")
	fl.BoolVarP(&f.yes, "yes", "y", false, "do not ask for confirmation")
	return cmd
}

func (a *app) chooser() target.Chooser {
	if !a.interactive {
		return nil
	}
	return func(prompt string, options []string) (int, error) {
		picked, err := ui.Pick(options, prompt)
		if err != nil {
			return 0, err
		}
		for i, o := range options {
			if o == picked {
				return i, nil
			}
		}
		return 0, fmt.Errorf("selection not found")
	}
}

func (a *app) confirmYN(prompt string) bool {
	fmt.Fprint(a.err, prompt)
	line, _ := bufio.NewReader(a.in).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true
	}
	return false
}

func (a *app) runSh(ctx context.Context, ref string, remote []string, f shFlags) error {
	if f.debug && len(remote) > 0 {
		// Silently running the command in the application's own container would
		// do the opposite of what --debug asks for.
		return fmt.Errorf("--debug opens an interactive debug container and cannot be combined with a command after \"--\"")
	}
	cfg, err := a.opts.REST()
	if err != nil {
		return err
	}
	cs, err := a.opts.Clientset()
	if err != nil {
		return err
	}
	ns, err := a.opts.ResolvedNamespace()
	if err != nil {
		return err
	}
	ctxName, err := a.opts.ContextName()
	if err != nil {
		return err
	}
	if len(remote) == 0 && !a.interactive {
		return fmt.Errorf("an interactive shell needs a terminal; to run one command use: kubekaze sh <pod> -- <command>")
	}

	pod, err := target.ResolvePod(ctx, cs, ns, ref, f.selector, a.chooser())
	if err != nil {
		return err
	}
	if pod.Status.Phase != corev1.PodRunning {
		return fmt.Errorf("pod %s is %s, not Running", pod.Name, pod.Status.Phase)
	}
	container, err := target.ResolveContainer(pod, f.container, a.chooser())
	if err != nil {
		return err
	}

	prod := kube.IsProduction(ctxName)
	where := fmt.Sprintf("%s/%s/%s (container %s)", ctxName, ns, pod.Name, container)
	if prod && !f.yes {
		if !a.interactive {
			return fmt.Errorf("production-looking context %q: pass --yes to confirm", ctxName)
		}
		if !a.confirmYN(a.pal.Red("Production-looking context.") + fmt.Sprintf(" Open a session in %s? [y/N]: ", term.SafeText(where))) {
			return ui.ErrCancelled
		}
	}

	if len(remote) > 0 {
		return a.execCommand(ctx, cfg, cs, ns, pod.Name, container, remote)
	}

	if f.debug {
		return a.debugSession(ctx, cfg, cs, ns, pod.Name, container, f)
	}

	var sh []string
	if f.shell != "" {
		sh = strings.Fields(f.shell)
	} else {
		probe := func(ctx context.Context, cmd []string) error {
			return execQuiet(ctx, cfg, cs, ns, pod.Name, container, cmd)
		}
		sh, err = shell.Find(ctx, probe)
		if errors.Is(err, shell.ErrNoShell) {
			fmt.Fprintln(a.err, a.pal.Yellow("No shell found in container "+term.SafeText(container)+"."))
			if !f.yes && !a.confirmYN(fmt.Sprintf("Start an ephemeral debug container (%s)? It stays in the pod until the pod is deleted. [y/N]: ", debugImageFrom(f.debugImage))) {
				return ui.ErrCancelled
			}
			return a.debugSession(ctx, cfg, cs, ns, pod.Name, container, f)
		}
		if err != nil {
			return err
		}
	}
	fmt.Fprintln(a.err, a.pal.Dim(fmt.Sprintf("Using %s in %s", strings.Join(sh, " "), term.SafeText(where))))

	u, err := subresourceURL(cs, ns, pod.Name, "exec", &corev1.PodExecOptions{
		Container: container, Command: sh, Stdin: true, Stdout: true, TTY: true,
	})
	if err != nil {
		return err
	}
	return a.finishStream(streamTTY(ctx, cfg, http.MethodPost, u, os.Stdin, os.Stdout, os.Stderr))
}

func (a *app) debugSession(ctx context.Context, cfg *rest.Config, cs kubernetes.Interface, ns, pod, target string, f shFlags) error {
	image := debugImageFrom(f.debugImage)
	if err := checkDebugImage(image); err != nil {
		return err
	}
	if f.debug && !f.yes && !a.confirmYN(fmt.Sprintf("Start an ephemeral debug container (%s) in %s? It stays in the pod until the pod is deleted. [y/N]: ", image, term.SafeText(pod))) {
		return ui.ErrCancelled
	}
	fmt.Fprintln(a.err, a.pal.Dim("Starting debug container..."))
	name, err := startDebugContainer(ctx, cs, ns, pod, target, image)
	if err != nil {
		return err
	}
	u, err := subresourceURL(cs, ns, pod, "attach", &corev1.PodAttachOptions{
		Container: name, Stdin: true, Stdout: true, TTY: true,
	})
	if err != nil {
		return err
	}
	fmt.Fprintln(a.err, a.pal.Dim("Attached to "+name+" (shares process namespace with "+term.SafeText(target)+")"))
	return a.finishStream(streamTTY(ctx, cfg, http.MethodPost, u, os.Stdin, os.Stdout, os.Stderr))
}

func (a *app) execCommand(ctx context.Context, cfg *rest.Config, cs kubernetes.Interface, ns, pod, container string, cmd []string) error {
	u, err := subresourceURL(cs, ns, pod, "exec", &corev1.PodExecOptions{
		Container: container, Command: cmd, Stdout: true, Stderr: true,
	})
	if err != nil {
		return err
	}
	ex, err := newExecutor(cfg, http.MethodPost, u)
	if err != nil {
		return err
	}
	return a.finishStream(ex.StreamWithContext(ctx, streamOpts(a)))
}

// finishStream turns a remote non-zero exit into a silent exit code.
func (a *app) finishStream(err error) error {
	if err == nil {
		return nil
	}
	var ce utilexec.CodeExitError
	if errors.As(err, &ce) {
		return exitCodeError{code: ce.Code}
	}
	return err
}
