package cli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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

func genCompletion(root *cobra.Command, shell, alias string) ([]byte, error) {
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
	if err != nil {
		return nil, err
	}
	return withAlias(b.Bytes(), shell, alias)
}

var aliasName = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

// withAlias makes the completion work for a shell alias such as
// alias kk=kubekaze. Shells do not do this on their own: completion is bound to
// the command name, not to what an alias expands to.
func withAlias(script []byte, shell, alias string) ([]byte, error) {
	if alias == "" {
		return script, nil
	}
	if !aliasName.MatchString(alias) {
		return nil, fmt.Errorf("invalid alias %q (letters, digits, '_', '-' and '.' only)", alias)
	}
	var extra string
	switch shell {
	case "bash":
		// The generated script runs "${words[0]} __complete", i.e. the alias
		// itself. A shell alias is not available there (and may not even exist
		// in the shell that loads this), so the lookup would fail silently and
		// bash would fall back to file names. Ask the real command instead.
		script = bytes.Replace(script,
			[]byte(`requestComp="${words[0]} __complete ${args[*]}"`),
			[]byte(fmt.Sprintf(`local cmd="${words[0]}"
    [[ $cmd == %s ]] && cmd=kubekaze
    requestComp="${cmd} __complete ${args[*]}"`, alias)),
			1)
		extra = fmt.Sprintf(`
# alias %[1]s
if [[ $(type -t compopt) = "builtin" ]]; then
    complete -o default -F __start_kubekaze %[1]s
else
    complete -o default -o nospace -F __start_kubekaze %[1]s
fi
`, alias)
	case "zsh":
		script = bytes.Replace(script, []byte("#compdef kubekaze\n"), []byte("#compdef kubekaze "+alias+"\n"), 1)
		extra = fmt.Sprintf("\n# alias %[1]s\n(( $+functions[compdef] )) && compdef _kubekaze %[1]s\n", alias)
	case "fish":
		extra = fmt.Sprintf("\n# alias %[1]s\ncomplete -c %[1]s -w kubekaze\n", alias)
	case "powershell":
		extra = fmt.Sprintf("\n# alias %[1]s\nRegister-ArgumentCompleter -CommandName '%[1]s' -ScriptBlock ${__kubekazeCompleterBlock}\n", alias)
	}
	return append(script, extra...), nil
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

Using an alias (alias kk=kubekaze)? Shells bind completion to the command name,
so the alias needs to be named explicitly:

  source <(kubekaze completion bash --alias kk)
  kubekaze completion install --alias kk

Pod, namespace and context names are looked up live from the cluster you are
pointed at, so completion needs a working kubeconfig (it stays quiet if not).`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error { return cmd.Help() },
	}
	for _, sh := range shells {
		sh := sh
		var alias string
		sc := &cobra.Command{
			Use:   sh,
			Short: "Print the " + sh + " completion script",
			Long: fmt.Sprintf(`Print the %s completion script on standard output.

If you use a shell alias for kubekaze, completion does not follow the alias on
its own. Name the alias with --alias and it is wired up too:

  alias kk=kubekaze
  %s`, sh, aliasLoadLine(sh)),
			Example: aliasExample(sh),
			Args:    cobra.NoArgs,
			RunE: func(*cobra.Command, []string) error {
				b, err := genCompletion(root, sh, alias)
				if err != nil {
					return err
				}
				_, err = a.out.Write(b)
				return err
			},
		}
		sc.Flags().StringVar(&alias, "alias", "", "also enable completion for this alias, e.g. kk")
		cmd.AddCommand(sc)
	}
	var force bool
	var installAlias string
	install := &cobra.Command{
		Use:   "install [bash|zsh|fish|powershell]",
		Short: "Install tab completion for your shell",
		Long: `Write the completion script to your user directory and tell you what, if
anything, is left to do. The shell is detected from $SHELL unless given.

Files written ($XDG_DATA_HOME defaults to ~/.local/share, $XDG_CONFIG_HOME to ~/.config):

  bash        $XDG_DATA_HOME/bash-completion/completions/kubekaze
              (needs the bash-completion package; loaded on first use)
  zsh         $XDG_DATA_HOME/zsh/site-functions/_kubekaze
              (the directory must be in fpath; the command tells you the line to add)
  fish        $XDG_CONFIG_HOME/fish/completions/kubekaze.fish
  powershell  nothing is written; add one line to your $PROFILE (shown)

With --alias kk, bash and fish also get a tiny file named after the alias
(.../completions/kk, .../completions/kk.fish), because both load completion files
by the name you type. zsh lists the alias inside its single file.

Nothing outside your home directory is touched, and an existing file that does
not look like a kubekaze completion is left alone unless --force is given.

Prefer your own startup file? Skip install and add one line to ~/.bashrc (or
~/.zshrc) instead:  source <(kubekaze completion bash --alias kk)`,
		Example: `  # detect the shell and install
  kubekaze completion install

  # a specific shell
  kubekaze completion install zsh

  # you use alias kk=kubekaze: make completion work for kk too
  kubekaze completion install --alias kk`,
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
			}, force, installAlias)
			if err != nil {
				return err
			}
			if res.path != "" {
				fmt.Fprintf(a.out, "%s wrote %s\n", a.pal.Green("OK"), res.path)
			}
			for _, e := range res.extra {
				fmt.Fprintf(a.out, "%s wrote %s\n", a.pal.Green("OK"), e)
			}
			fmt.Fprintln(a.out, res.note)
			return nil
		},
	}
	install.Flags().StringVar(&installAlias, "alias", "", "also enable completion for this shell alias, e.g. kk")
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

type installResult struct {
	path  string
	extra []string // additional files written for an alias
	note  string
}

// installCompletion writes the completion script for shell below the user's
// directories. It is separate from the command so that it can be tested.
func installCompletion(root *cobra.Command, shell string, d dirs, force bool, alias string) (installResult, error) {
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
		line := "kubekaze completion powershell"
		if alias != "" {
			line += " --alias " + alias
		}
		return installResult{note: "PowerShell has no standard completion directory. Add this line to your profile ($PROFILE):\n\n  " + line + " | Out-String | Invoke-Expression"}, nil
	default:
		return installResult{}, fmt.Errorf("unsupported shell %q (supported: %s)", shell, strings.Join(shells, ", "))
	}
	script, err := genCompletion(root, shell, alias)
	if err != nil {
		return installResult{}, err
	}
	if err := writeOwnFile(path, script, force); err != nil {
		return installResult{}, err
	}
	res := installResult{path: path, note: note}

	// bash-completion and fish load completion files lazily, by the name of the
	// command being completed: typing "kk<TAB>" looks for a file named kk, not
	// kubekaze. So an alias needs a small file of its own that pulls in the real one.
	if alias != "" {
		var extra string
		var body string
		switch shell {
		case "bash":
			extra = filepath.Join(filepath.Dir(path), alias)
			body = fmt.Sprintf("# kubekaze: completion for the alias %s (loads the real script)\n. %s\n", alias, shQuote(filepath.ToSlash(path)))
		case "fish":
			extra = filepath.Join(filepath.Dir(path), alias+".fish")
			body = fmt.Sprintf("# kubekaze: completion for the alias %s\ncomplete -c %s -w kubekaze\n", alias, alias)
		}
		if extra != "" {
			if err := writeOwnFile(extra, []byte(body), force); err != nil {
				return installResult{}, err
			}
			res.extra = append(res.extra, extra)
		}
	}
	return res, nil
}

// writeOwnFile writes data atomically. It refuses to replace an existing file
// that does not mention kubekaze (someone else's file) unless force is set.
func writeOwnFile(path string, data []byte, force bool) error {
	if existing, err := os.ReadFile(path); err == nil && !force && !bytes.Contains(existing, []byte("kubekaze")) {
		return fmt.Errorf("%s exists and does not look like a kubekaze completion; use --force to replace it", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ---- dynamic completion ----------------------------------------------------

const completeTimeout = 3 * time.Second

func complCtx(cmd *cobra.Command) (context.Context, context.CancelFunc) {
	base := context.Background()
	if cmd != nil && cmd.Context() != nil {
		base = cmd.Context()
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
	"job": "job", "jobs": "job",
	"node": "node", "nodes": "node", "no": "node",
}

// completeTargets completes pod names and workload references (deploy/x).
func (a *app) completeTargets(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return a.targetNames(cmd, toComplete, true, false)
}

// completeWhyTargets is completeTargets plus nodes: why also explains nodes.
func (a *app) completeWhyTargets(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return a.targetNames(cmd, toComplete, true, true)
}

func (a *app) targetNames(cmd *cobra.Command, toComplete string, workloads, nodes bool) ([]string, cobra.ShellCompDirective) {
	ctx, cancel := complCtx(cmd)
	defer cancel()
	cs, err := a.clientset()
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
		case "job":
			l, err := cs.BatchV1().Jobs(ns).List(ctx, metav1.ListOptions{})
			if err == nil {
				for _, o := range l.Items {
					names = append(names, o.Name)
				}
			}
		case "node":
			if !nodes {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
			l, err := cs.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
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
		out = append(out, "deploy/", "sts/", "ds/", "job/")
		if nodes {
			out = append(out, "node/", "nodes")
		}
		dir |= cobra.ShellCompDirectiveNoSpace
	}
	sort.Strings(out)
	return filterPrefix(out, toComplete), dir
}

func (a *app) completeRmArgs(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) == 0 {
		return filterPrefix([]string{"pods"}, toComplete), cobra.ShellCompDirectiveNoFileComp
	}
	return a.targetNames(cmd, toComplete, false, false)
}

func fixed(vals ...string) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return cobra.FixedCompletions(vals, cobra.ShellCompDirectiveNoFileComp)
}

func fixedArgs(vals ...string) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return func(_ *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return vals, cobra.ShellCompDirectiveNoFileComp
	}
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
	set("why", a.completeWhyTargets)
	set("rm", a.completeRmArgs)
	set("top", fixedArgs("ns"))

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
	flag("top", "sort", fixed("cpu", "memory"))
	flag("top", "limit", fixed("5", "10", "20", "50"))
	flag("top", "interval", fixed("2s", "5s", "10s", "30s"))
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

func aliasLoadLine(shell string) string {
	switch shell {
	case "bash":
		return "source <(kubekaze completion bash --alias kk)"
	case "zsh":
		return "source <(kubekaze completion zsh --alias kk)"
	case "fish":
		return "kubekaze completion fish --alias kk | source"
	}
	return "kubekaze completion powershell --alias kk | Out-String | Invoke-Expression"
}

func aliasExample(shell string) string {
	return "  # without an alias\n  " + strings.Replace(aliasLoadLine(shell), " --alias kk", "", 1) +
		"\n\n  # with: alias kk=kubekaze\n  " + aliasLoadLine(shell)
}

// shQuote single-quotes s for a POSIX shell.
func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
