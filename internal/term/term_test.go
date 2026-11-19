package term

import "testing"

func TestSafeText(t *testing.T) {
	cases := map[string]string{
		"plain text":        "plain text",
		"tab\tand\nnewline": "tab\tand\nnewline",
		"\x1b[31mred":       "\\x1b[31mred",
		"bell\x07":          "bell\\x07",
		"del\x7f":           "del\\x7f",
		"한글 ok":             "한글 ok",
	}
	for in, want := range cases {
		if got := SafeText(in); got != want {
			t.Errorf("SafeText(%q) = %q, want %q", in, got, want)
		}
	}
}
