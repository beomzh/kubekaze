// Package shell finds a working interactive shell inside a container.
package shell

import (
	"context"
	"errors"
)

// Candidates are tried in order. Using bare names lets the container's PATH
// decide, which also covers /bin, /usr/bin and busybox layouts.
var Candidates = [][]string{
	{"bash"},
	{"sh"},
	{"ash"},
	{"busybox", "sh"},
}

// ErrNoShell means no candidate could be started.
var ErrNoShell = errors.New("no shell found in the container (distroless or scratch image?)")

// Probe runs a command non-interactively in the container and returns nil
// when it exits with status 0.
type Probe func(ctx context.Context, cmd []string) error

// Find returns the first shell command that works.
func Find(ctx context.Context, probe Probe) ([]string, error) {
	for _, c := range Candidates {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		test := append(append([]string{}, c...), "-c", "exit 0")
		if err := probe(ctx, test); err == nil {
			return c, nil
		}
	}
	return nil, ErrNoShell
}
