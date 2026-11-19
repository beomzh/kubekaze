//go:build windows

package term

import (
	"os"

	"golang.org/x/sys/windows"
)

// EnableVT turns on ANSI escape processing for f (needed for remote shells on
// Windows consoles). The returned function restores the previous mode.
func EnableVT(f *os.File) func() {
	h := windows.Handle(f.Fd())
	var mode uint32
	if err := windows.GetConsoleMode(h, &mode); err != nil {
		return func() {}
	}
	if err := windows.SetConsoleMode(h, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING); err != nil {
		return func() {}
	}
	return func() { _ = windows.SetConsoleMode(h, mode) }
}
