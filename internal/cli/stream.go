package cli

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"

	xterm "golang.org/x/term"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"

	"github.com/beomzh/kubekaze/internal/term"
)

// newExecutor builds an executor that prefers WebSocket and falls back to SPDY,
// like kubectl does.
func newExecutor(cfg *rest.Config, method string, u *url.URL) (remotecommand.Executor, error) {
	spdy, err := remotecommand.NewSPDYExecutor(cfg, method, u)
	if err != nil {
		return nil, err
	}
	ws, err := remotecommand.NewWebSocketExecutor(cfg, http.MethodGet, u.String())
	if err != nil {
		return nil, err
	}
	return remotecommand.NewFallbackExecutor(ws, spdy, func(err error) bool {
		return err != nil // fall back to SPDY if the WebSocket handshake fails
	})
}

func subresourceURL(cs kubernetes.Interface, ns, pod, sub string, opts runtime.Object) (*url.URL, error) {
	type restGetter interface{ RESTClient() rest.Interface }
	rg, ok := cs.CoreV1().(restGetter)
	if !ok {
		return nil, fmt.Errorf("client does not expose a REST client")
	}
	req := rg.RESTClient().Post().Resource("pods").Namespace(ns).Name(pod).SubResource(sub)
	req.VersionedParams(opts, scheme.ParameterCodec)
	return req.URL(), nil
}

// execQuiet runs cmd in the container without a TTY and returns its error.
func execQuiet(ctx context.Context, cfg *rest.Config, cs kubernetes.Interface, ns, pod, container string, cmd []string) error {
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
	probeCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	return ex.StreamWithContext(probeCtx, remotecommand.StreamOptions{Stdout: io.Discard, Stderr: io.Discard})
}

// sizeQueue reports terminal size changes. It polls, which works on every OS
// (SIGWINCH does not exist on Windows).
type sizeQueue struct {
	ch   chan remotecommand.TerminalSize
	done chan struct{}
}

func newSizeQueue(f *os.File) *sizeQueue {
	q := &sizeQueue{ch: make(chan remotecommand.TerminalSize, 1), done: make(chan struct{})}
	get := func() (remotecommand.TerminalSize, bool) {
		w, h, err := xterm.GetSize(int(f.Fd()))
		return remotecommand.TerminalSize{Width: uint16(w), Height: uint16(h)}, err == nil
	}
	if s, ok := get(); ok {
		q.ch <- s
	}
	go func() {
		last, _ := get()
		t := time.NewTicker(400 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-q.done:
				return
			case <-t.C:
				if s, ok := get(); ok && s != last {
					last = s
					select {
					case q.ch <- s:
					default:
					}
				}
			}
		}
	}()
	return q
}

func (q *sizeQueue) Next() *remotecommand.TerminalSize {
	select {
	case s := <-q.ch:
		return &s
	case <-q.done:
		return nil
	}
}

func (q *sizeQueue) stop() { close(q.done) }

// streamTTY connects the local terminal to a remote exec or attach session.
func streamTTY(ctx context.Context, cfg *rest.Config, method string, u *url.URL, stdin *os.File, stdout, stderr *os.File) error {
	ex, err := newExecutor(cfg, method, u)
	if err != nil {
		return err
	}
	fd := int(stdin.Fd())
	old, err := xterm.MakeRaw(fd)
	if err != nil {
		return fmt.Errorf("putting terminal in raw mode: %w", err)
	}
	defer func() { _ = xterm.Restore(fd, old) }()
	restoreVT := term.EnableVT(stdout)
	defer restoreVT()

	q := newSizeQueue(stdout)
	defer q.stop()
	return ex.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdin: stdin, Stdout: stdout, Tty: true, TerminalSizeQueue: q,
	})
}

func streamOpts(a *app) remotecommand.StreamOptions {
	return remotecommand.StreamOptions{Stdout: a.out, Stderr: a.err}
}
