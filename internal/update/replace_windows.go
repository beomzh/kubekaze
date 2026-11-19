//go:build windows

package update

import "os"

// Replace swaps the running executable. Windows cannot overwrite or delete a
// running .exe, but it can rename it, so the old one is moved aside first.
func Replace(exe, staged string) error {
	old := exe + ".old"
	_ = os.Remove(old) // left over from an earlier update, if it is no longer running
	if err := os.Rename(exe, old); err != nil {
		return err
	}
	if err := os.Rename(staged, exe); err != nil {
		_ = os.Rename(old, exe) // put the old one back
		return err
	}
	_ = os.Remove(old) // fails while it is still running; removed by the next update
	return nil
}
