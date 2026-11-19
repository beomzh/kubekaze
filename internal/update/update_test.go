package update

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestParseAndCompareVersion(t *testing.T) {
	v := func(s string) Version {
		x, err := ParseVersion(s)
		if err != nil {
			t.Fatalf("ParseVersion(%q): %v", s, err)
		}
		return x
	}
	if v("v0.1.1").String() != "0.1.1" || v("0.1.1").Tag() != "v0.1.1" {
		t.Error("String/Tag wrong")
	}
	cases := []struct {
		a, b string
		want int
	}{
		{"0.1.0", "0.1.1", -1}, {"0.2.0", "0.1.9", 1}, {"1.0.0", "1.0.0", 0},
		{"0.1.1-rc.1", "0.1.1", -1}, {"0.1.1", "0.1.1-rc.1", 1}, {"0.1.1-rc.1", "0.1.1-rc.2", -1}, {"0.10.0", "0.9.0", 1},
	}
	for _, c := range cases {
		if got := v(c.a).Compare(v(c.b)); got != c.want {
			t.Errorf("Compare(%s,%s) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
	for _, bad := range []string{"", "1.2", "latest", "v1.2.x", "../1.2.3"} {
		if _, err := ParseVersion(bad); err == nil {
			t.Errorf("ParseVersion(%q) should fail", bad)
		}
	}
	if ValidTag("v1.2.3/../x") || !ValidTag("v1.2.3-rc.1") {
		t.Error("ValidTag wrong")
	}
}

func TestArchiveNames(t *testing.T) {
	v, _ := ParseVersion("0.1.1")
	if got := ArchiveName(v, "linux", "amd64"); got != "kubekaze_0.1.1_linux_amd64.tar.gz" {
		t.Error(got)
	}
	if got := ArchiveName(v, "windows", "arm64"); got != "kubekaze_0.1.1_windows_arm64.zip" {
		t.Error(got)
	}
	pv, goos, arch, err := ParseArchiveName("kubekaze_0.2.0-rc.1_darwin_arm64.tar.gz")
	if err != nil || pv.String() != "0.2.0-rc.1" || goos != "darwin" || arch != "arm64" {
		t.Errorf("%v %s %s %v", pv, goos, arch, err)
	}
	for _, bad := range []string{"kubekaze.tar.gz", "other_0.1.1_linux_amd64.tar.gz", "kubekaze_0.1.1_plan9_amd64.zip"} {
		if _, _, _, err := ParseArchiveName(bad); err == nil {
			t.Errorf("%q should be rejected", bad)
		}
	}
}

// ---- helpers: build a release in a temp dir ----

func makeTarGz(t *testing.T, path, entry string, content []byte) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for _, e := range []struct {
		name string
		body []byte
	}{{"LICENSE", []byte("license")}, {entry, content}} {
		_ = tw.WriteHeader(&tar.Header{Name: e.name, Mode: 0o755, Size: int64(len(e.body)), Typeflag: tar.TypeReg})
		_, _ = tw.Write(e.body)
	}
	tw.Close()
	gz.Close()
	f.Close()
}

func makeZip(t *testing.T, path, entry string, content []byte) {
	t.Helper()
	f, _ := os.Create(path)
	zw := zip.NewWriter(f)
	w, _ := zw.Create(entry)
	_, _ = w.Write(content)
	zw.Close()
	f.Close()
}

func sumLine(t *testing.T, path string) string {
	t.Helper()
	b, _ := os.ReadFile(path)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]) + "  " + filepath.Base(path) + "\n"
}

func release(t *testing.T, dir, ver, goos, goarch string, bin []byte) (archive, sums string) {
	t.Helper()
	v, _ := ParseVersion(ver)
	archive = filepath.Join(dir, ArchiveName(v, goos, goarch))
	if goos == "windows" {
		makeZip(t, archive, "kubekaze.exe", bin)
	} else {
		makeTarGz(t, archive, "kubekaze", bin)
	}
	sums = filepath.Join(dir, "checksums.txt")
	if err := os.WriteFile(sums, []byte(sumLine(t, archive)), 0o644); err != nil {
		t.Fatal(err)
	}
	return archive, sums
}

func noSanity(string, Version) error { return nil }

func TestChecksumsAndVerify(t *testing.T) {
	dir := t.TempDir()
	archive, sums := release(t, dir, "0.1.1", "linux", "amd64", []byte("bin"))
	f, _ := os.Open(sums)
	cs, err := ParseChecksums(f)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyFile(archive, cs); err != nil {
		t.Errorf("valid file rejected: %v", err)
	}
	if err := os.WriteFile(archive, []byte("tampered"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := VerifyFile(archive, cs); err == nil || !strings.Contains(err.Error(), "mismatch") {
		t.Errorf("tampered file accepted: %v", err)
	}
	if err := VerifyFile(filepath.Join(dir, "unknown.tar.gz"), cs); err == nil {
		t.Error("a file that is not listed must be rejected")
	}
	if _, err := ParseChecksums(strings.NewReader("not a checksum file")); err == nil {
		t.Error("garbage checksums must be rejected")
	}
}

func TestExtractBinary(t *testing.T) {
	dir := t.TempDir()
	tgz := filepath.Join(dir, "a.tar.gz")
	makeTarGz(t, tgz, "kubekaze", []byte("hello"))
	dest := filepath.Join(dir, "out")
	if err := ExtractBinary(tgz, "linux", dest); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(dest); string(b) != "hello" {
		t.Errorf("content = %q", b)
	}
	z := filepath.Join(dir, "a.zip")
	makeZip(t, z, "kubekaze.exe", []byte("exe"))
	if err := ExtractBinary(z, "windows", dest); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(dest); string(b) != "exe" {
		t.Errorf("zip content = %q", b)
	}
	other := filepath.Join(dir, "b.tar.gz")
	makeTarGz(t, other, "something-else", []byte("x"))
	if err := ExtractBinary(other, "linux", dest); err == nil {
		t.Error("an archive without the binary must fail")
	}
}

func newUpdater(t *testing.T, exe string) *Updater {
	t.Helper()
	return &Updater{Client: NewClient("", "test"), Exe: exe, GOOS: runtime.GOOS, GOARCH: runtime.GOARCH, Sanity: noSanity}
}

func TestApplyArchiveReplacesBinary(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "kubekaze")
	if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	archive, sums := release(t, t.TempDir(), "0.1.1", runtime.GOOS, runtime.GOARCH, []byte("new"))
	if runtime.GOOS == "windows" {
		t.Skip("covered by the windows build; extraction name differs")
	}
	v, err := newUpdater(t, exe).ApplyArchive(archive, sums)
	if err != nil || v.String() != "0.1.1" {
		t.Fatalf("%v %v", v, err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "new" {
		t.Errorf("binary not replaced: %q", b)
	}
	if info, _ := os.Stat(exe); info.Mode().Perm()&0o111 == 0 {
		t.Error("the new binary must stay executable")
	}
	left, _ := filepath.Glob(filepath.Join(dir, ".kubekaze-update-*"))
	if len(left) != 0 {
		t.Errorf("temporary files left behind: %v", left)
	}
}

func TestApplyArchiveFailuresLeaveBinaryUntouched(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix file semantics")
	}
	dir := t.TempDir()
	exe := filepath.Join(dir, "kubekaze")
	_ = os.WriteFile(exe, []byte("old"), 0o755)
	untouched := func(what string) {
		t.Helper()
		if b, _ := os.ReadFile(exe); string(b) != "old" {
			t.Errorf("%s: the binary was changed", what)
		}
		if left, _ := filepath.Glob(filepath.Join(dir, ".kubekaze-update-*")); len(left) != 0 {
			t.Errorf("%s: temporary files left: %v", what, left)
		}
	}
	rel := t.TempDir()
	archive, sums := release(t, rel, "0.1.1", runtime.GOOS, runtime.GOARCH, []byte("new"))

	// checksum mismatch
	if err := os.WriteFile(sums, []byte(strings.Repeat("0", 64)+"  "+filepath.Base(archive)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := newUpdater(t, exe).ApplyArchive(archive, sums); err == nil || !strings.Contains(err.Error(), "mismatch") {
		t.Errorf("mismatch: %v", err)
	}
	untouched("checksum mismatch")

	// wrong platform
	other := "arm64"
	if runtime.GOARCH == "arm64" {
		other = "amd64"
	}
	a2, s2 := release(t, t.TempDir(), "0.1.1", runtime.GOOS, other, []byte("new"))
	if _, err := newUpdater(t, exe).ApplyArchive(a2, s2); err == nil || !strings.Contains(err.Error(), "matching archive") {
		t.Errorf("platform: %v", err)
	}
	untouched("wrong platform")

	// the new binary fails its sanity run
	a3, s3 := release(t, t.TempDir(), "0.1.1", runtime.GOOS, runtime.GOARCH, []byte("new"))
	u := newUpdater(t, exe)
	u.Sanity = func(string, Version) error { return fmt.Errorf("exec format error") }
	if _, err := u.ApplyArchive(a3, s3); err == nil || !strings.Contains(err.Error(), "nothing was changed") {
		t.Errorf("sanity: %v", err)
	}
	untouched("sanity failure")

	// missing checksums file
	if _, err := newUpdater(t, exe).ApplyArchive(a3, filepath.Join(rel, "nope.txt")); err == nil {
		t.Error("missing checksums must fail")
	}
	untouched("missing checksums")
}

func TestStageExplainsPermissionProblem(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a non-root unix user")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0o755)
	_, err := Stage(filepath.Join(dir, "kubekaze"))
	if err == nil || !strings.Contains(err.Error(), "sudo") {
		t.Errorf("err = %v", err)
	}
}

func TestManagedBy(t *testing.T) {
	if ManagedBy("/opt/homebrew/Cellar/kubekaze/0.1.0/bin/kubekaze") == "" || ManagedBy("/home/u/.krew/bin/kubectl-kubekaze") == "" {
		t.Error("package-manager installs must be detected")
	}
	if ManagedBy("/usr/local/bin/kubekaze") != "" || ManagedBy("/bin/kubekaze") != "" {
		t.Error("plain installs must not be flagged")
	}
}

// ---- client ----

func TestClientGitHubLatestAndDownload(t *testing.T) {
	rel := t.TempDir()
	archive, sums := release(t, rel, "0.1.1", "linux", "amd64", []byte("new"))
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/"+Repo+"/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"tag_name":"v0.1.1"}`)
	})
	mux.HandleFunc("/v0.1.1/", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, filepath.Join(rel, filepath.Base(r.URL.Path)))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := NewClient("", "test")
	c.APIURL = srv.URL
	tag, err := c.Latest(context.Background())
	if err != nil || tag != "v0.1.1" {
		t.Fatalf("Latest = %q, %v", tag, err)
	}

	// a mirror laid out like the release
	m := NewClient(srv.URL, "test")
	dest := filepath.Join(t.TempDir(), "x")
	if err := m.Fetch(context.Background(), "v0.1.1", filepath.Base(archive), dest, 1<<20); err != nil {
		t.Fatal(err)
	}
	if got, _ := SHA256File(dest); got == "" || !strings.Contains(sumLine(t, archive), got) {
		t.Error("downloaded file differs from the release")
	}
	if err := m.Fetch(context.Background(), "v0.1.1", filepath.Base(archive), dest, 10); err == nil {
		t.Error("the size limit must be enforced")
	}
	if err := m.Fetch(context.Background(), "v0.1.1", "missing.tar.gz", dest, 1<<20); err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("404: %v", err)
	}
	if err := m.Fetch(context.Background(), "../etc", "passwd", dest, 10); err == nil {
		t.Error("an invalid tag must be rejected before any request")
	}
	_ = sums
}

func TestMirrorLatestFile(t *testing.T) {
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, " v0.3.0\n") }))
	defer ok.Close()
	if tag, err := NewClient(ok.URL, "t").Latest(context.Background()); err != nil || tag != "v0.3.0" {
		t.Errorf("%q %v", tag, err)
	}
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "../../evil") }))
	defer bad.Close()
	if _, err := NewClient(bad.URL, "t").Latest(context.Background()); err == nil {
		t.Error("a latest file that is not a tag must be rejected")
	}
	none := httptest.NewServer(http.NotFoundHandler())
	defer none.Close()
	if _, err := NewClient(none.URL, "t").Latest(context.Background()); err == nil || !strings.Contains(err.Error(), "--version") {
		t.Errorf("a mirror without a latest file should explain --version: %v", err)
	}
}

func TestRefusesPlainHTTPToRemoteHosts(t *testing.T) {
	for _, raw := range []string{"http://example.com/x", "http://10.0.0.5/x", "ftp://example.com/x"} {
		u, _ := url.Parse(raw)
		if err := checkURL(u); err == nil {
			t.Errorf("%s must be refused", raw)
		}
	}
	for _, raw := range []string{"https://example.com/x", "http://127.0.0.1:9/x", "http://localhost/x", "http://[::1]/x"} {
		u, _ := url.Parse(raw)
		if err := checkURL(u); err != nil {
			t.Errorf("%s must be allowed: %v", raw, err)
		}
	}
}

func TestDownloadAndApplyEndToEnd(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix path")
	}
	rel := t.TempDir()
	release(t, rel, "0.1.1", runtime.GOOS, runtime.GOARCH, []byte("brand new"))
	mux := http.NewServeMux()
	mux.HandleFunc("/v0.1.1/", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, filepath.Join(rel, filepath.Base(r.URL.Path)))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	exe := filepath.Join(t.TempDir(), "kubekaze")
	_ = os.WriteFile(exe, []byte("old"), 0o755)
	u := newUpdater(t, exe)
	u.Client = NewClient(srv.URL, "test")
	v, err := u.DownloadAndApply(context.Background(), "v0.1.1")
	if err != nil || v.String() != "0.1.1" {
		t.Fatalf("%v %v", v, err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "brand new" {
		t.Errorf("content = %q", b)
	}
}
