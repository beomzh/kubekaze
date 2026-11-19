// Package version holds build information injected at link time.
package version

import "fmt"

// These are set via -ldflags by GoReleaser.
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

// String returns a human readable version line.
func String() string {
	return fmt.Sprintf("%s (commit %s, built %s)", Version, Commit, Date)
}
