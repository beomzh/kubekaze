package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/beomzh/kubekaze/internal/events"
	"github.com/beomzh/kubekaze/internal/redact"
	"github.com/beomzh/kubekaze/internal/target"
	"github.com/beomzh/kubekaze/internal/term"
)

type logsFlags struct {
	container  string
	selector   string
	follow     bool
	tail       int64
	since      string
	noPrevious bool
	noRedact   bool
	noPrefix   bool
	timestamps bool
	plainOut   bool
}

func (a *app) newLogsCmd() *cobra.Command {
	var f logsFlags
	cmd := &cobra.Command{
		Use:   "logs [pod | deploy/name | ...]",
		Short: "Tail logs of many pods at once; includes the previous run of restarted containers",
		Long: `Tail logs of one pod, every pod of a workload, or a label selector, like stern,
with crash awareness: when a container restarted, its previous run is printed
first (what kubectl logs -p does) so the reason for the crash is not lost.
Errors and warnings are highlighted. Common secrets are masked unless
--no-redact is given.

  kubekaze logs api-7d9f
  kubekaze logs deploy/api -f --since 10m
  kubekaze logs -l app=web -c nginx --tail 50
  kubekaze logs deploy/api --no-previous

Only pods that exist when the command starts are followed.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref := ""
			if len(args) == 1 {
				ref = args[0]
			}
			return a.runLogs(cmd.Context(), ref, f)
		},
	}
	fl := cmd.Flags()
	fl.StringVarP(&f.container, "container", "c", "", "container name (default: all containers)")
	fl.StringVarP(&f.selector, "selector", "l", "", "label selector")
	fl.BoolVarP(&f.follow, "follow", "f", false, "follow new log lines")
	fl.Int64Var(&f.tail, "tail", 100, "lines to show per container (-1 for all)")
	fl.StringVar(&f.since, "since", "", "only logs newer than this (30s, 10m, 2h)")
	fl.BoolVar(&f.noPrevious, "no-previous", false, "do not print the previous run of restarted containers")
	fl.BoolVar(&f.noRedact, "no-redact", false, "do not mask secrets")
	fl.BoolVar(&f.noPrefix, "no-prefix", false, "do not prefix lines with pod/container")
	fl.BoolVar(&f.timestamps, "timestamps", false, "include timestamps")
	return cmd
}

var (
	errLine  = regexp.MustCompile(`(?i)\b(error|err|fatal|panic|exception|traceback|critical|fail(ed|ure)?)\b`)
	warnLine = regexp.MustCompile(`(?i)\b(warn(ing)?)\b`)
)

// lineLevel classifies a log line for highlighting.
func lineLevel(l string) string {
	switch {
	case errLine.MatchString(l):
		return "error"
	case warnLine.MatchString(l):
		return "warn"
	}
	return ""
}

type logSource struct {
	pod       corev1.Pod
	container string
	previous  bool
	color     int
}

func (a *app) runLogs(ctx context.Context, ref string, f logsFlags) error {
	cs, err := a.opts.Clientset()
	if err != nil {
		return err
	}
	ns, err := a.opts.ResolvedNamespace()
	if err != nil {
		return err
	}
	var sinceSec *int64
	if f.since != "" {
		d, err := events.ParseSince(f.since)
		if err != nil || d <= 0 {
			return fmt.Errorf("invalid --since %q (examples: 30s, 10m, 2h)", f.since)
		}
		s := int64(d.Seconds())
		sinceSec = &s
	}
	pods, err := a.logPods(ctx, cs, ns, ref, f.selector)
	if err != nil {
		return err
	}
	sources := logSources(pods, f)
	if len(sources) == 0 {
		return fmt.Errorf("no containers to read logs from")
	}
	multi := len(sources) > 1

	var mu sync.Mutex
	var wg sync.WaitGroup
	emit := func(src logSource, line string) {
		if !f.noRedact {
			line = redact.Line(line)
		}
		line = term.SafeText(line)
		switch lineLevel(line) {
		case "error":
			line = a.pal.Red(line)
		case "warn":
			line = a.pal.Yellow(line)
		}
		prefix := ""
		if !f.noPrefix && (multi || src.previous) {
			label := src.pod.Name + "/" + src.container
			if src.previous {
				label += " (previous)"
			}
			prefix = a.pal.Pick(src.color, term.SafeText(label)) + " | "
		}
		mu.Lock()
		fmt.Fprintln(a.out, prefix+line)
		mu.Unlock()
	}

	read := func(src logSource, follow bool) {
		defer wg.Done()
		opts := &corev1.PodLogOptions{Container: src.container, Previous: src.previous, Follow: follow, Timestamps: f.timestamps, SinceSeconds: sinceSec}
		if f.tail >= 0 {
			t := f.tail
			opts.TailLines = &t
		}
		stream, err := cs.CoreV1().Pods(src.pod.Namespace).GetLogs(src.pod.Name, opts).Stream(ctx)
		if err != nil {
			mu.Lock()
			fmt.Fprintf(a.err, "%s %s/%s: %s\n", a.pal.Yellow("warning:"), term.SafeText(src.pod.Name), term.SafeText(src.container), term.SafeText(err.Error()))
			mu.Unlock()
			return
		}
		defer stream.Close()
		streamLines(stream, func(l string) { emit(src, l) })
	}

	// Previous runs first and sequentially, so the crash reason leads the output.
	for _, src := range sources {
		if src.previous {
			wg.Add(1)
			read(src, false)
		}
	}
	for _, src := range sources {
		if src.previous {
			continue
		}
		wg.Add(1)
		go read(src, f.follow)
	}
	wg.Wait()
	return nil
}

func streamLines(r io.Reader, fn func(string)) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for sc.Scan() {
		fn(sc.Text())
	}
}

// logSources lists what to read: for each container, its previous run first
// (when it restarted) and then the current run.
func logSources(pods []corev1.Pod, f logsFlags) []logSource {
	var out []logSource
	for pi, p := range pods {
		for _, c := range p.Spec.Containers {
			if f.container != "" && c.Name != f.container {
				continue
			}
			if !f.noPrevious {
				for _, st := range p.Status.ContainerStatuses {
					if st.Name == c.Name && st.RestartCount > 0 && st.LastTerminationState.Terminated != nil {
						out = append(out, logSource{pod: p, container: c.Name, previous: true, color: pi})
					}
				}
			}
			out = append(out, logSource{pod: p, container: c.Name, color: pi})
		}
	}
	return out
}

func (a *app) logPods(ctx context.Context, cs kubernetes.Interface, ns, ref, selector string) ([]corev1.Pod, error) {
	if ref == "" && selector == "" {
		return nil, fmt.Errorf("give a pod, a workload (deploy/api) or -l selector")
	}
	if ref != "" {
		kind, name, err := target.ParseRef(ref)
		if err != nil {
			return nil, err
		}
		if kind == "pod" {
			p, err := cs.CoreV1().Pods(ns).Get(ctx, name, metav1.GetOptions{})
			if err != nil {
				return nil, fmt.Errorf("getting pod %s/%s: %w", ns, name, err)
			}
			return []corev1.Pod{*p}, nil
		}
		selector, err = target.Selector(ctx, cs, ns, kind, name)
		if err != nil {
			return nil, err
		}
	}
	list, err := cs.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return nil, fmt.Errorf("listing pods: %w", err)
	}
	if len(list.Items) == 0 {
		return nil, fmt.Errorf("no pods found in namespace %q for %q", ns, strings.TrimSpace(ref+" "+selector))
	}
	return list.Items, nil
}

var _ = time.Second
