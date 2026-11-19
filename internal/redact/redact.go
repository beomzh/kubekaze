// Package redact masks common secrets in text before it is shown or sent
// anywhere. It is best effort and must not be the only line of defence.
package redact

import "regexp"

const mask = "[REDACTED]"

var rules = []struct {
	re   *regexp.Regexp
	repl string
}{
	// key=value / key: value style assignments for secret-looking keys
	{regexp.MustCompile(`(?i)\b([\w.-]*(?:password|passwd|pwd|secret|token|api[_-]?key|access[_-]?key|private[_-]?key|credential)[\w.-]*)(["']?\s*[:=]\s*["']?)[^\s"',;&]+`), "${1}${2}" + mask},
	// Authorization headers and bearer tokens
	{regexp.MustCompile(`(?i)\b(authorization["']?\s*[:=]\s*["']?(?:bearer|basic)\s+)[A-Za-z0-9._~+/=-]+`), "${1}" + mask},
	{regexp.MustCompile(`(?i)\b(bearer\s+)[A-Za-z0-9._~+/=-]{12,}`), "${1}" + mask},
	// AWS access key ids and GitHub tokens
	{regexp.MustCompile(`\b(AKIA|ASIA)[A-Z0-9]{16}\b`), mask},
	{regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{30,}\b`), mask},
	// JWTs
	{regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\b`), mask},
	// credentials inside URLs: scheme://user:pass@host
	{regexp.MustCompile(`(\b[a-z][a-z0-9+.-]*://[^\s/:@]+:)[^\s/@]+(@)`), "${1}" + mask + "${2}"},
	// PEM private key headers
	{regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`), "-----BEGIN PRIVATE KEY----- " + mask},
}

// Line masks secrets in one line of text.
func Line(s string) string {
	for _, r := range rules {
		s = r.re.ReplaceAllString(s, r.repl)
	}
	return s
}
