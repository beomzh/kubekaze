//go:build !windows

package update

import "os"

// Replace atomically puts staged in place of exe. Both must be on the same
// filesystem; Stage guarantees that by creating the file next to exe.
func Replace(exe, staged string) error {
	return os.Rename(staged, exe)
}
