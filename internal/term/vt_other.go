//go:build !windows

package term

import "os"

// EnableVT is a no-op outside Windows.
func EnableVT(*os.File) func() { return func() {} }
