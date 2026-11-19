package cli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/beomzh/kubekaze/internal/events"
	"github.com/beomzh/kubekaze/internal/kubeconfig"
	"github.com/beomzh/kubekaze/internal/version"
)

var shells = []string{"bash", "zsh", "fish", "powershell"}

func genCompletion(root *cobra.Command, shell string) ([]byte, error) {
	var b bytes.Buffer
	var err error
	switch shell {
	case "bash":
		err = root.GenBashCompletionV2(&b, true)
	case "zsh":
		err = root.GenZshCompletion(&b)
	case "fish":
		err = root.GenFishCompletion(&b, true)
	case "powershell":
		err = root.GenPowerShellCompletionWithDesc(&b)
	default:
		return nil, fmt.Errorf("unsupported shell %q (supported: %s)", shell, strings.Join(shells, ", "))
	}
	return b.Bytes(), err
}

func (a *app) newCompletionCmd(root *cobra.Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "completion",
		Short: "Shell tab completion (bash, zsh, fish, powershell)",
		Long: `Tab completion for commands, flags, contexts, namespaces, pods and workloads.

Quick setup for your shell (writes one file in your user directory):

  kubekaze completion install

Or load it yourself:

  bash        source <(kubekaze completion bash)
  zsh         source <(kubekaze completion zsh)
  fish        kubekaze completion fish | source
  powershell  kubekaze completion powershell | Out-String | Invoke-Expression

Pod, namespace and context names are looked up live from the cluster you are
pointed at, so completion needs a working kubeconfig (it stays quiet if not).`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error { return cmd.Help() },
	}
	for _, sh := range shells {
		sh := sh
		cmd.AddCommand(&cobra.Command{
			Use:   sh,
			Short: "Print the " + sh + " completion script",
			Args:  cobra.NoArgs,
			RunE: func(*cobra.Command, []string) error {
				b, err := genCompletion(root, sh)
				if err != nil {
					return err
				}
				_, err = a.out.Write(b)
				return err
			},
		})
	}
	var force bool
	install := &cobra.Command{
		Use:   "install [bash|zsh|fish|powershell]",
		Short: "Install tab completion for your shell",
		Long: `Write the completion script to your user directory and tell you what, if
anything, is left to do. The shell is detected from $SHELL unless given.
Nothing outside your home directory is touched, and an existing file that does
not look like a kubekaze completion is left alone unless --force is given.`,
		Example: `  kubekaze completion install
  kubekaze completion install zsh`,
		Args:      cobra.MaximumNArgs(1),
		ValidArgs: shells,
		RunE: func(cmd *cobra.Command, args []string) error {
			sh := ""
			if len(args) == 1 {
				sh = args[0]
			}
			if sh == "" {
				sh = detectShell()
			}
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("finding your home directory: %w", err)
			}
			res, err := installCompletion(root, sh, dirs{
				home: home, dataHome: os.Getenv("XDG_DATA_HOME"), configHome: os.Getenv("XDG_CONFIG_HOME"),
			}, force)
			if err != nil {
				return err
			}
			if res.path != "" {
				fmt.Fprintf(a.out, "%s wrote %s\n", a.pal.Green("OK"), res.path)
			}
			fmt.Fprintln(a.out, res.note)
			return nil
		},
	}
	install.Flags().BoolVar(&force, "force", false, "overwrite an existing file that is not a kubekaze completion")
	cmd.AddCommand(install)
	return cmd
}

func detectShell() string {
	if s := os.Getenv("SHELL"); s != "" {
		return filepath.Base(s)
	}
	if runtime.GOOS == "windows" {
		return "powershell"
	}
	return ""
}

type dirs struct{ home, dataHome, configHome string }

type installResult struct{ path, note string }

// installCompletion writes the completion script for shell below the user's
// directories. It is separate from the command so that it can be tested.
func installCompletion(root *cobra.Command, shell string, d dirs, force bool) (installResult, error) {
	if shell == "" {
		return installResult{}, fmt.Errorf("could not detect your shell; run: kubekaze completion install <%s>", strings.Join(shells, "|"))
	}
	data := d.dataHome
	if data == "" {
		data = filepath.Join(d.home, ".local", "share")
	}
	conf := d.configHome
	if conf == "" {
		conf = filepath.Join(d.home, ".config")
	}
	var path, note string
	switch shell {
	case "bash":
		path = filepath.Join(data, "bash-completion", "completions", "kubekaze")
		note = "Open a new shell to use it. This needs the bash-completion package (v2) to be installed and loaded."
	case "zsh":
		path = filepath.Join(data, "zsh", "site-functions", "_kubekaze")
		dir := filepath.Dir(path)
		if rel, err := filepath.Rel(d.home, dir); err == nil && !strings.HasPrefix(rel, "..") {
			dir = "~/" + filepath.ToSlash(rel)
		}
		note = fmt.Sprintf("Add this to ~/.zshrc if it is not there yet, then open a new shell:\n\n  fpath=(%s $fpath)\n  autoload -Uz compinit && compinit", dir)
	case "fish":
		path = filepath.Join(conf, "fish", "completions", "kubekaze.fish")
		note = "Fish loads it automatically; open a new shell."
	case "powershell":
		return installResult{note: "PowerShell has no standard completion directory. Add this line to your profile ($PROFILE):\n\n  kubekaze completion powershell | Out-String | Invoke-Expression"}, nil
	default:
		return installResult{}, fmt.Errorf("unsupported shell %q (supported: %s)", shell, strings.Join(shells, ", "))
	}
	script, err := genCompletion(root, shell)
	if err != nil {
		return installResult{}, err
	}
	if existing, err := os.ReadFile(path); err == nil && !force && !bytes.Contains(existing, []byte("kubekaze")) {
		return installResult{}, fmt.Errorf("%s exists and does not look like a kubekaze completion; use --force to replace it", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return installResult{}, err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, script, 0o644); err != nil {
		return installResult{}, err
	}
	if err := os.Rename(tmp, path); err != nil {
		return installResult{}, err
	}
	return installResult{path: path, note: note}, nil
}

// ---- dynamic completion ----------------------------------------------------

const completeTimeout = 3 * time.Second

func complCtx(cmd *cobra.Command) (context.Context, context.CancelFunc) {
	base := cmd.Context()
	if base == nil {
		base = context.Background()
	}
	return context.WithTimeout(base, completeTimeout)
}

func filterPrefix(items []string, prefix string) []string {
	var out []string
	for _, s := range items {
		if strings.HasPrefix(s, prefix) {
			out = append(out, s)
		}
	}
	return out
}

func (a *app) completeContexts(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	names, _, err := kubeconfig.New(a.opts.Kubeconfig, "").Contexts()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return filterPrefix(names, toComplete), cobra.ShellCompDirectiveNoFileComp
}

func (a *app) completeNamespaces(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	ctx, cancel := complCtx(cmd)
	defer cancel()
	names, _, err := a.opts.ListNamespaces(ctx)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return filterPrefix(names, toComplete), cobra.ShellCompDirectiveNoFileComp
}

// completeFlagNamespace completes -n, which may hold a comma separated list.
func (a *app) completeFlagNamespace(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	head, last := "", toComplete
	if i := strings.LastIndex(toComplete, ","); i >= 0 {
		head, last = toComplete[:i+1], toComplete[i+1:]
	}
	ctx, cancel := complCtx(cmd)
	defer cancel()
	names, _, err := a.opts.ListNamespaces(ctx)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	var out []string
	for _, n := range filterPrefix(names, last) {
		out = append(out, head+n)
	}
	return out, cobra.ShellCompDirectiveNoFileComp
}

var workloadPrefixes = map[string]string{
	"deploy": "deployment", "deployment": "deployment",
	"sts": "statefulset", "statefulset": "statefulset",
	"ds": "daemonset", "daemonset": "daemonset",
}

// completeTargets completes pod names and workload references (deploy/x).
func (a *app) completeTargets(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return a.targetNames(cmd, toComplete, true)
}

func (a *app) targetNames(cmd *cobra.Command, toComplete string, workloads bool) ([]string, cobra.ShellCompDirective) {
	ctx, cancel := complCtx(cmd)
	defer cancel()
	cs, err := a.opts.Clientset()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	ns, err := a.opts.ResolvedNamespace()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	if kind, name, ok := strings.Cut(toComplete, "/"); ok && workloads {
		var names []string
		switch workloadPrefixes[strings.ToLower(kind)] {
		case "deployment":
			l, err := cs.AppsV1().Deployments(ns).List(ctx, metav1.ListOptions{})
			if err == nil {
				for _, o := range l.Items {
					names = append(names, o.Name)
				}
			}
		case "statefulset":
			l, err := cs.AppsV1().StatefulSets(ns).List(ctx, metav1.ListOptions{})
			if err == nil {
				for _, o := range l.Items {
					names = append(names, o.Name)
				}
			}
		case "daemonset":
			l, err := cs.AppsV1().DaemonSets(ns).List(ctx, metav1.ListOptions{})
			if err == nil {
				for _, o := range l.Items {
					names = append(names, o.Name)
				}
			}
		default:
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		var out []string
		for _, n := range filterPrefix(names, name) {
			out = append(out, kind+"/"+n)
		}
		return out, cobra.ShellCompDirectiveNoFileComp
	}
	l, err := cs.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	var out []string
	for _, p := range l.Items {
		out = append(out, p.Name)
	}
	dir := cobra.ShellCompDirectiveNoFileComp
	if workloads {
		out = append(out, "deploy/", "sts/", "ds/")
		dir |= cobra.ShellCompDirectiveNoSpace
	}
	sort.Strings(out)
	return filterPrefix(out, toComplete), dir
}

func (a *app) completeRmArgs(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) == 0 {
		return filterPrefix([]string{"pods"}, toComplete), cobra.ShellCompDirectiveNoFileComp
	}
	return a.targetNames(cmd, toComplete, false)
}

func fixed(vals ...string) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return cobra.FixedCompletions(vals, cobra.ShellCompDirectiveNoFileComp)
}

// registerCompletions wires dynamic completion into the command tree.
func (a *app) registerCompletions(root *cobra.Command) {
	_ = root.RegisterFlagCompletionFunc("context", func(c *cobra.Command, args []string, tc string) ([]string, cobra.ShellCompDirective) {
		return a.completeContexts(c, nil, tc)
	})
	_ = root.RegisterFlagCompletionFunc("namespace", a.completeFlagNamespace)
	_ = root.RegisterFlagCompletionFunc("output", fixed("text", "json"))

	byName := map[string]*cobra.Command{}
	for _, c := range root.Commands() {
		byName[c.Name()] = c
	}
	set := func(name string, fn func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective)) {
		if c := byName[name]; c != nil {
			c.ValidArgsFunction = fn
		}
	}
	set("ctx", a.completeContexts)
	set("ns", a.completeNamespaces)
	set("sh", a.completeTargets)
	set("logs", a.completeTargets)
	set("why", a.completeTargets)
	set("rm", a.completeRmArgs)

	flag := func(cmd, name string, fn func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective)) {
		if c := byName[cmd]; c != nil {
			_ = c.RegisterFlagCompletionFunc(name, fn)
		}
	}
	statuses := fixed("CrashLoopBackOff", "ImagePullBackOff", "ErrImagePull", "Error", "Evicted", "OOMKilled", "Completed", "Pending", "Terminating", "Failed", "Unknown")
	flag("rm", "status", statuses)
	flag("events", "sort", fixed(events.SortKeys...))
	flag("events", "type", fixed("Warning", "Normal"))
	flag("events", "since", fixed("10m", "30m", "1h", "2h", "1d"))
	flag("rm", "older-than", fixed("10m", "30m", "1h", "2h", "1d"))
	flag("logs", "since", fixed("1m", "10m", "30m", "1h", "2h"))
}

func (a *app) newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version",
		Args:  cobra.NoArgs,
		Run:   func(*cobra.Command, []string) { fmt.Fprintf(a.out, "kubekaze %s\n", version.String()) },
	}
}
