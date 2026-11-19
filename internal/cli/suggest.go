package cli

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// exitUsage is the exit code for wrong commands, flags or arguments.
const exitUsage = 2

// levenshtein returns the edit distance between a and b.
func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur := make([]int, len(rb)+1)
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(rb)]
}

// closest returns candidates that look like word: a small edit distance, or a
// shared prefix. Best matches first.
func closest(word string, candidates []string) []string {
	type scored struct {
		s string
		d int
	}
	word = strings.ToLower(word)
	var out []scored
	for _, c := range candidates {
		lc := strings.ToLower(c)
		d := levenshtein(word, lc)
		switch {
		case strings.HasPrefix(lc, word) && len(word) >= 2:
			out = append(out, scored{c, 0})
		case d <= 2 || d <= len(lc)/3:
			out = append(out, scored{c, d})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].d < out[j].d })
	res := make([]string, 0, len(out))
	for _, o := range out {
		res = append(res, o.s)
	}
	if len(res) > 3 {
		res = res[:3]
	}
	return res
}

var unknownFlag = regexp.MustCompile(`unknown flag: --([\w-]+)`)

// flagError adds "did you mean" help to flag parsing errors.
func (a *app) flagError(cmd *cobra.Command, err error) error {
	m := unknownFlag.FindStringSubmatch(err.Error())
	if m == nil {
		return err
	}
	var names []string
	collect := func(f *pflag.Flag) {
		if !f.Hidden {
			names = append(names, f.Name)
		}
	}
	cmd.LocalFlags().VisitAll(collect)
	cmd.InheritedFlags().VisitAll(collect)
	if sug := closest(m[1], names); len(sug) > 0 {
		for i := range sug {
			sug[i] = "--" + sug[i]
		}
		return fmt.Errorf("%w\n\nDid you mean this?\n\t%s", err, strings.Join(sug, "\n\t"))
	}
	return err
}

// usageHint tells the user where to look after a usage error.
func usageHint(cmd *cobra.Command) string {
	if cmd == nil || !cmd.HasParent() {
		return "Run 'kubekaze --help' to see all commands."
	}
	return fmt.Sprintf("Usage: %s\nRun '%s --help' for details and examples.", cmd.UseLine(), cmd.CommandPath())
}
