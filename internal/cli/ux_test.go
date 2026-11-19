package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func run(args ...string) (code int, out, errs string) {
	var o, e bytes.Buffer
	code = Execute(args, strings.NewReader(""), &o, &e)
	return code, o.String(), e.String()
}

func TestLevenshteinAndClosest(t *testing.T) {
	if levenshtein("kitten", "sitting") != 3 || levenshtein("", "abc") != 3 || levenshtein("same", "same") != 0 {
		t.Error("levenshtein wrong")
	}
	got := closest("stauts", []string{"status", "events", "logs"})
	if len(got) != 1 || got[0] != "status" {
		t.Errorf("closest = %v", got)
	}
	if got := closest("zzzzzz", []string{"status", "events"}); len(got) != 0 {
		t.Errorf("unrelated word should not match: %v", got)
	}
	if got := closest("nam", []string{"namespace", "output"}); len(got) == 0 || got[0] != "namespace" {
		t.Errorf("prefix should match: %v", got)
	}
}

func TestUnknownCommandSuggests(t *testing.T) {
	code, _, errs := run("stauts")
	if code != exitUsage {
		t.Errorf("exit = %d, want %d", code, exitUsage)
	}
	for _, want := range []string{"unknown command", "status", "kubekaze --help"} {
		if !strings.Contains(errs, want) {
			t.Errorf("stderr missing %q:\n%s", want, errs)
		}
	}
}

func TestUnknownFlagSuggests(t *testing.T) {
	code, _, errs := run("events", "--sumary")
	if code != exitUsage {
		t.Errorf("exit = %d, want %d", code, exitUsage)
	}
	for _, want := range []string{"unknown flag: --sumary", "Did you mean", "--summary", "kubekaze events --help"} {
		if !strings.Contains(errs, want) {
			t.Errorf("stderr missing %q:\n%s", want, errs)
		}
	}
}

func TestMissingArgShowsUsage(t *testing.T) {
	code, _, errs := run("why")
	if code != exitUsage || !strings.Contains(errs, "Usage: kubekaze why") {
		t.Errorf("exit=%d stderr:\n%s", code, errs)
	}
}

func TestRuntimeErrorHasNoUsageHint(t *testing.T) {
	code, _, errs := run("--kubeconfig", filepath.Join(t.TempDir(), "missing"), "ns", "-c")
	if code == 0 || strings.Contains(errs, "Usage:") {
		t.Errorf("exit=%d stderr:\n%s", code, errs)
	}
	if code == exitUsage {
		t.Error("a runtime failure must not use the usage exit code")
	}
}

func TestHelpIsGroupedWithExamples(t *testing.T) {
	code, out, _ := run("--help")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	for _, want := range []string{"Troubleshooting:", "Daily use:", "Setup:", "status", "completion", "Examples:", "kubekaze completion install"} {
		if !strings.Contains(out, want) {
			t.Errorf("root help missing %q:\n%s", want, out)
		}
	}
	for _, c := range []string{"status", "why", "events", "logs", "sh", "rm", "ctx", "ns"} {
		_, out, _ := run(c, "--help")
		if !strings.Contains(out, "Examples:") || !strings.Contains(out, "kubekaze "+c) {
			t.Errorf("%s --help has no example:\n%s", c, out)
		}
	}
}

func TestCompletionScripts(t *testing.T) {
	for _, sh := range shells {
		code, out, errs := run("completion", sh)
		if code != 0 || !strings.Contains(out, "kubekaze") || len(out) < 200 {
			t.Errorf("%s: exit=%d len=%d err=%s", sh, code, len(out), errs)
		}
	}
	if code, _, _ := run("completion", "tcsh"); code != exitUsage {
		t.Errorf("unknown shell should be a usage error, got %d", code)
	}
}

func TestInstallCompletion(t *testing.T) {
	root := (&app{}).newRoot()
	home := t.TempDir()
	d := dirs{home: home}

	res, err := installCompletion(root, "bash", d, false)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, ".local", "share", "bash-completion", "completions", "kubekaze")
	if res.path != want {
		t.Errorf("path = %s, want %s", res.path, want)
	}
	if b, _ := os.ReadFile(want); !strings.Contains(string(b), "kubekaze") {
		t.Error("script not written")
	}
	if _, err := installCompletion(root, "bash", d, false); err != nil {
		t.Errorf("re-install over our own file must work: %v", err)
	}

	// a foreign file is not overwritten without --force
	if err := os.WriteFile(want, []byte("# something else\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := installCompletion(root, "bash", d, false); err == nil {
		t.Error("foreign file must not be overwritten")
	}
	if _, err := installCompletion(root, "bash", d, true); err != nil {
		t.Errorf("--force should overwrite: %v", err)
	}

	for _, sh := range []string{"zsh", "fish"} {
		r, err := installCompletion(root, sh, d, false)
		if err != nil || !strings.HasPrefix(r.path, home) {
			t.Errorf("%s: %v %+v", sh, err, r)
		}
	}
	if r, err := installCompletion(root, "zsh", d, false); err != nil || !strings.Contains(r.note, "fpath") {
		t.Errorf("zsh note should explain fpath: %+v %v", r, err)
	}
	if r, err := installCompletion(root, "powershell", d, false); err != nil || r.path != "" || !strings.Contains(r.note, "$PROFILE") {
		t.Errorf("powershell: %+v %v", r, err)
	}
	if _, err := installCompletion(root, "", d, false); err == nil {
		t.Error("empty shell should explain how to choose one")
	}
}

func TestDynamicContextCompletion(t *testing.T) {
	kc := filepath.Join(t.TempDir(), "config")
	cfg := `apiVersion: v1
kind: Config
current-context: dev
clusters: [{name: c, cluster: {server: "https://127.0.0.1:1"}}]
users: [{name: u, user: {token: x}}]
contexts:
- {name: dev, context: {cluster: c, user: u}}
- {name: prod-eks, context: {cluster: c, user: u}}
`
	if err := os.WriteFile(kc, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	code, out, _ := run("--kubeconfig", kc, "__complete", "ctx", "p")
	if code != 0 || !strings.Contains(out, "prod-eks") || strings.Contains(out, "dev") {
		t.Errorf("ctx completion: exit=%d\n%s", code, out)
	}
	code, out, _ = run("__complete", "events", "--sort", "")
	if code != 0 || !strings.Contains(out, "count") || !strings.Contains(out, "namespace") {
		t.Errorf("--sort completion: exit=%d\n%s", code, out)
	}
	code, out, _ = run("__complete", "rm", "")
	if code != 0 || !strings.Contains(out, "pods") {
		t.Errorf("rm completion: exit=%d\n%s", code, out)
	}
}
