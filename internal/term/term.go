// Package term contains terminal helpers: output sanitising and color.
package term

import (
	"fmt"
	"io"
	"os"
	"strings"
	"unicode"

	xterm "golang.org/x/term"
)

// SafeText escapes control characters (including ESC) in strings that come
// from the cluster, so that event messages, logs or object names cannot inject
// terminal escape sequences. Tabs and newlines are preserved.
func SafeText(s string) string {
	if !needsEscape(s) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + 8)
	for _, r := range s {
		if r != '\n' && r != '\t' && (unicode.IsControl(r) || r == 0x7f) {
			fmt.Fprintf(&b, "\\x%02x", r)
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func needsEscape(s string) bool {
	for _, r := range s {
		if r != '\n' && r != '\t' && (unicode.IsControl(r) || r == 0x7f) {
			return true
		}
	}
	return false
}

// IsTerminal reports whether f is an interactive terminal.
func IsTerminal(f any) bool {
	fd, ok := f.(interface{ Fd() uintptr })
	if !ok {
		return false
	}
	return xterm.IsTerminal(int(fd.Fd()))
}

// Palette decides whether to emit ANSI colors.
type Palette struct{ enabled bool }

// NewPalette enables colors only for terminals, honouring NO_COLOR.
func NewPalette(w io.Writer) Palette {
	if _, no := os.LookupEnv("NO_COLOR"); no {
		return Palette{}
	}
	return Palette{enabled: IsTerminal(w) && os.Getenv("TERM") != "dumb"}
}

func (p Palette) wrap(code, s string) string {
	if !p.enabled {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

// Red, Yellow, Green, Cyan, Bold and Dim colorise s when enabled.
func (p Palette) Red(s string) string    { return p.wrap("31", s) }
func (p Palette) Yellow(s string) string { return p.wrap("33", s) }
func (p Palette) Green(s string) string  { return p.wrap("32", s) }
func (p Palette) Cyan(s string) string   { return p.wrap("36", s) }
func (p Palette) Bold(s string) string   { return p.wrap("1", s) }
func (p Palette) Dim(s string) string    { return p.wrap("2", s) }

// Pick returns s in the i-th color of a rotating set, so that lines from
// different pods are easy to tell apart.
func (p Palette) Pick(i int, s string) string {
	codes := []string{"36", "33", "32", "35", "34", "96", "93", "92"}
	return p.wrap(codes[((i%len(codes))+len(codes))%len(codes)], s)
}
