package cli

import (
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/beomzh/kubekaze/internal/term"
)

// cell is one table cell; style colours the text after padding is applied so
// ANSI codes never disturb column alignment.
type cell struct {
	text  string
	style func(string) string
}

func plain(s string) cell { return cell{text: term.SafeText(s)} }

// table prints rows with space-padded columns. The last column is not padded.
func table(w io.Writer, header []string, rows [][]cell) {
	widths := make([]int, len(header))
	for i, h := range header {
		widths[i] = utf8.RuneCountInString(h)
	}
	for _, r := range rows {
		for i, c := range r {
			if n := utf8.RuneCountInString(firstLine(c.text)); n > widths[i] {
				widths[i] = n
			}
		}
	}
	var sb strings.Builder
	line := func(cells []cell) {
		sb.Reset()
		for i, c := range cells {
			t := firstLine(c.text)
			pad := ""
			if i < len(cells)-1 {
				pad = strings.Repeat(" ", widths[i]-utf8.RuneCountInString(t)+2)
			}
			if c.style != nil {
				t = c.style(t)
			}
			sb.WriteString(t)
			sb.WriteString(pad)
		}
		fmt.Fprintln(w, sb.String())
	}
	hc := make([]cell, len(header))
	for i, h := range header {
		hc[i] = cell{text: h}
	}
	line(hc)
	for _, r := range rows {
		line(r)
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i] + " …"
	}
	return s
}
