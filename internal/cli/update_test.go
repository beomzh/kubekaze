package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/beomzh/kubekaze/internal/term"
	"github.com/beomzh/kubekaze/internal/update"
	"github.com/beomzh/kubekaze/internal/version"
)

func setVersion(t *testing.T, v string) {
	t.Helper()
	old := version.Version
	version.Version = v
	t.Cleanup(func() { version.Version = old })
}

// publish writes a release (archive + checksums.txt) for the current platform.
func publish(t *testing.T, dir, ver string, bin []byte) string {
	t.Helper()
	name := fmt.Sprintf("kubekaze_%s_%s_%s.tar.gz", ver, runtime.GOOS, runtime.GOARCH)
	path := filepath.Join(dir, name)
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	_ = tw.WriteHeader(&tar.Header{Name: "kubekaze", Mode: 0o755, Size: int64(len(bin)), Typeflag: tar.TypeReg})
	_, _ = tw.Write(bin)
	tw.Close()
	gz.Close()
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256(buf.Bytes())
	if err := os.WriteFile(filepath.Join(dir, "checksums.txt"), []byte(hex.EncodeToString(h[:])+"  "+name+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func updApp(t *testing.T, exe string, interactive bool, in string) (*app, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	var out, errb bytes.Buffer
	return &app{
		in: strings.NewReader(in), out: &out, err: &errb, pal: term.Palette{}, interactive: interactive, output: "text",
		exe:    func() (string, error) { return exe, nil },
		sanity: func(string, update.Version) error { return nil },
	}, &out, &errb
}

func mirror(t *testing.T, dir, latest string) string {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/latest", func(w http.ResponseWriter, r *http.Request) { fmt.Fprintln(w, latest) })
	mux.HandleFunc("/"+latest+"/", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, filepath.Join(dir, filepath.Base(r.URL.Path)))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL
}

func newExe(t *testing.T) string {
	t.Helper()
	exe := filepath.Join(t.TempDir(), "kubekaze")
	if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	return exe
}

func content(t *testing.T, p string) string {
	t.Helper()
	b, _ := os.ReadFile(p)
	return string(b)
}

func skipWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("archives are .zip on Windows; the unix path is tested here")
	}
}

func TestUpdateCheck(t *testing.T) {
	setVersion(t, "0.1.0")
	rel := t.TempDir()
	url := mirror(t, rel, "v0.1.1")
	a, out, _ := updApp(t, newExe(t), false, "")
	if err := a.runUpdate(context.Background(), updateFlags{check: true, url: url}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "v0.1.1") || !strings.Contains(out.String(), "kubekaze update") {
		t.Errorf("output: %s", out.String())
	}
	out.Reset()
	a.output = "json"
	_ = a.runUpdate(context.Background(), updateFlags{check: true, url: url})
	if !strings.Contains(out.String(), `"updateAvailable": true`) || !strings.Contains(out.String(), `"latest": "v0.1.1"`) {
		t.Errorf("json: %s", out.String())
	}

	setVersion(t, "0.1.1")
	out.Reset()
	a.output = "text"
	_ = a.runUpdate(context.Background(), updateFlags{check: true, url: url})
	if !strings.Contains(out.String(), "Up to date") {
		t.Errorf("up to date: %s", out.String())
	}
}

func TestUpdateFromMirror(t *testing.T) {
	skipWindows(t)
	setVersion(t, "0.1.0")
	rel := t.TempDir()
	publish(t, rel, "0.1.1", []byte("NEW"))
	url := mirror(t, rel, "v0.1.1")
	exe := newExe(t)
	a, out, _ := updApp(t, exe, false, "")
	if err := a.runUpdate(context.Background(), updateFlags{url: url, yes: true}); err != nil {
		t.Fatal(err)
	}
	if content(t, exe) != "NEW" || !strings.Contains(out.String(), "v0.1.0 -> v0.1.1") {
		t.Errorf("exe=%q out=%s", content(t, exe), out.String())
	}
}

func TestUpdateAlreadyCurrentChangesNothing(t *testing.T) {
	skipWindows(t)
	setVersion(t, "0.1.1")
	rel := t.TempDir()
	publish(t, rel, "0.1.1", []byte("NEW"))
	exe := newExe(t)
	a, out, _ := updApp(t, exe, false, "")
	if err := a.runUpdate(context.Background(), updateFlags{url: mirror(t, rel, "v0.1.1"), yes: true}); err != nil {
		t.Fatal(err)
	}
	if content(t, exe) != "old" || !strings.Contains(out.String(), "Already up to date") {
		t.Errorf("exe=%q out=%s", content(t, exe), out.String())
	}
}

func TestUpdateOfflineFrom(t *testing.T) {
	skipWindows(t)
	setVersion(t, "0.1.0")
	rel := t.TempDir()
	archive := publish(t, rel, "0.1.1", []byte("OFFLINE"))
	exe := newExe(t)
	a, out, _ := updApp(t, exe, false, "")
	if err := a.runUpdate(context.Background(), updateFlags{from: archive, yes: true}); err != nil {
		t.Fatal(err)
	}
	if content(t, exe) != "OFFLINE" || !strings.Contains(out.String(), "Updated") {
		t.Errorf("exe=%q out=%s", content(t, exe), out.String())
	}

	// an older archive is not installed without --force
	older := publish(t, t.TempDir(), "0.0.9", []byte("OLDER"))
	a, out, _ = updApp(t, exe, false, "")
	if err := a.runUpdate(context.Background(), updateFlags{from: older, yes: true}); err != nil {
		t.Fatal(err)
	}
	if content(t, exe) != "OFFLINE" || !strings.Contains(out.String(), "Nothing to do") {
		t.Errorf("an older archive must not replace a newer binary: %q %s", content(t, exe), out.String())
	}
}

func TestUpdateOfflineProblems(t *testing.T) {
	skipWindows(t)
	setVersion(t, "0.1.0")
	exe := newExe(t)
	rel := t.TempDir()
	archive := publish(t, rel, "0.1.1", []byte("X"))

	// no checksums.txt
	if err := os.Remove(filepath.Join(rel, "checksums.txt")); err != nil {
		t.Fatal(err)
	}
	a, _, _ := updApp(t, exe, false, "")
	if err := a.runUpdate(context.Background(), updateFlags{from: archive, yes: true}); err == nil || !strings.Contains(err.Error(), "checksums") {
		t.Errorf("missing checksums should be explained: %v", err)
	}
	// wrong file name
	if err := a.runUpdate(context.Background(), updateFlags{from: filepath.Join(rel, "random.tar.gz"), yes: true}); err == nil {
		t.Error("an unknown archive name must be rejected")
	}
	// tampered archive
	rel2 := t.TempDir()
	archive2 := publish(t, rel2, "0.1.1", []byte("X"))
	if err := os.WriteFile(archive2, []byte("tampered"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := a.runUpdate(context.Background(), updateFlags{from: archive2, yes: true}); err == nil || !strings.Contains(err.Error(), "mismatch") {
		t.Errorf("tampering must be detected: %v", err)
	}
	if content(t, exe) != "old" {
		t.Error("the binary must be untouched after every failure")
	}
}

func TestUpdateGuards(t *testing.T) {
	skipWindows(t)
	rel := t.TempDir()
	archive := publish(t, rel, "0.1.1", []byte("NEW"))

	// development build
	setVersion(t, "dev")
	exe := newExe(t)
	a, _, _ := updApp(t, exe, false, "")
	if err := a.runUpdate(context.Background(), updateFlags{from: archive, yes: true}); err == nil || !strings.Contains(err.Error(), "development build") {
		t.Errorf("dev build: %v", err)
	}
	if err := a.runUpdate(context.Background(), updateFlags{from: archive, yes: true, force: true}); err != nil {
		t.Errorf("--force should replace a dev build: %v", err)
	}

	// Homebrew install
	setVersion(t, "0.1.0")
	brew := filepath.Join(t.TempDir(), "Cellar", "kubekaze", "0.1.0", "bin", "kubekaze")
	_ = os.MkdirAll(filepath.Dir(brew), 0o755)
	_ = os.WriteFile(brew, []byte("old"), 0o755)
	a, _, _ = updApp(t, brew, false, "")
	if err := a.runUpdate(context.Background(), updateFlags{from: archive, yes: true}); err == nil || !strings.Contains(err.Error(), "brew upgrade") {
		t.Errorf("brew: %v", err)
	}
	if content(t, brew) != "old" {
		t.Error("a package-manager binary must not be replaced")
	}

	// no terminal and no --yes
	exe2 := newExe(t)
	a, _, _ = updApp(t, exe2, false, "y\n")
	if err := a.runUpdate(context.Background(), updateFlags{from: archive}); err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Errorf("non-interactive: %v", err)
	}
	if content(t, exe2) != "old" {
		t.Error("nothing may change without confirmation")
	}
}

func TestUpdateConfirmation(t *testing.T) {
	skipWindows(t)
	setVersion(t, "0.1.0")
	archive := publish(t, t.TempDir(), "0.1.1", []byte("NEW"))
	exe := newExe(t)
	a, _, _ := updApp(t, exe, true, "n\n")
	if err := a.runUpdate(context.Background(), updateFlags{from: archive}); err == nil || content(t, exe) != "old" {
		t.Errorf("answering n must abort without changes: %v", err)
	}
	a, _, _ = updApp(t, exe, true, "y\n")
	if err := a.runUpdate(context.Background(), updateFlags{from: archive}); err != nil || content(t, exe) != "NEW" {
		t.Errorf("answering y must update: %v %q", err, content(t, exe))
	}
}

func TestUpdateOfflineHintWhenNetworkFails(t *testing.T) {
	setVersion(t, "0.1.0")
	dead := httptest.NewServer(http.NotFoundHandler())
	url := dead.URL
	dead.Close()
	a, _, _ := updApp(t, newExe(t), false, "")
	err := a.runUpdate(context.Background(), updateFlags{url: url, yes: true})
	if err == nil || !strings.Contains(err.Error(), "--from") {
		t.Errorf("an unreachable server should point to --from: %v", err)
	}
}

func TestUpdateHelp(t *testing.T) {
	code, out, _ := run("update", "--help")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	for _, want := range []string{"--from", "--url", "--check", "air-gapped", "latest", "sudo", "cosign", "Examples:"} {
		if !strings.Contains(out, want) {
			t.Errorf("update --help missing %q", want)
		}
	}
	if _, root, _ := run("--help"); !strings.Contains(root, "update") {
		t.Error("update must be listed in the root help")
	}
}
