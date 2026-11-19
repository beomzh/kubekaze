package update

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Updater installs a release over the running binary.
type Updater struct {
	Client *Client
	// Exe is the binary to replace (the running executable, symlinks resolved).
	Exe          string
	GOOS, GOARCH string
	// Sanity runs the new binary before it is put in place. Tests replace it.
	Sanity func(path string, want Version) error
}

// ManagedBy names the package manager that owns exe, or "" if none. Replacing
// such a binary behind the package manager's back would confuse it.
func ManagedBy(exe string) string {
	p := filepath.ToSlash(exe)
	switch {
	case strings.Contains(p, "/Cellar/") || strings.Contains(p, "/linuxbrew/") || strings.Contains(p, "/homebrew/"):
		return "Homebrew: use `brew upgrade kubekaze`"
	case strings.Contains(p, "/.krew/"):
		return "krew: use `kubectl krew upgrade`"
	}
	return ""
}

// DownloadAndApply fetches the release archive and checksums for tag into a
// temporary directory and installs it.
func (u *Updater) DownloadAndApply(ctx context.Context, tag string) (Version, error) {
	v, err := ParseVersion(tag)
	if err != nil {
		return Version{}, err
	}
	dir, err := os.MkdirTemp("", "kubekaze-update-")
	if err != nil {
		return Version{}, err
	}
	defer os.RemoveAll(dir)

	name := ArchiveName(v, u.GOOS, u.GOARCH)
	archive := filepath.Join(dir, name)
	sumsPath := filepath.Join(dir, "checksums.txt")
	if err := u.Client.Fetch(ctx, tag, "checksums.txt", sumsPath, 1<<20); err != nil {
		return Version{}, fmt.Errorf("downloading checksums: %w", err)
	}
	if err := u.Client.Fetch(ctx, tag, name, archive, maxBinary); err != nil {
		return Version{}, fmt.Errorf("downloading %s: %w", name, err)
	}
	return u.ApplyArchive(archive, sumsPath)
}

// ApplyArchive verifies a release archive against its checksums file and
// installs it. It is also the offline path (kubekaze update --from).
func (u *Updater) ApplyArchive(archive, checksumsPath string) (Version, error) {
	v, goos, goarch, err := ParseArchiveName(filepath.Base(archive))
	if err != nil {
		return Version{}, err
	}
	if goos != u.GOOS || goarch != u.GOARCH {
		return Version{}, fmt.Errorf("%s is for %s/%s but this machine is %s/%s; download the matching archive", filepath.Base(archive), goos, goarch, u.GOOS, u.GOARCH)
	}
	f, err := os.Open(checksumsPath)
	if err != nil {
		return Version{}, fmt.Errorf("opening the checksums file: %w", err)
	}
	sums, err := ParseChecksums(f)
	f.Close()
	if err != nil {
		return Version{}, err
	}
	if err := VerifyFile(archive, sums); err != nil {
		return Version{}, err
	}

	staged, err := Stage(u.Exe)
	if err != nil {
		return Version{}, err
	}
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(staged)
		}
	}()
	if err := ExtractBinary(archive, u.GOOS, staged); err != nil {
		return Version{}, err
	}
	if info, err := os.Stat(u.Exe); err == nil {
		_ = os.Chmod(staged, info.Mode().Perm()|0o111)
	}
	sanity := u.Sanity
	if sanity == nil {
		sanity = RunSanity
	}
	if err := sanity(staged, v); err != nil {
		return Version{}, fmt.Errorf("the new binary does not run correctly, nothing was changed: %w", err)
	}
	if err := Replace(u.Exe, staged); err != nil {
		return Version{}, fmt.Errorf("replacing %s: %w", u.Exe, err)
	}
	ok = true
	return v, nil
}

// Stage creates an empty file next to exe, so that the final rename is atomic
// and fails early, with a useful message, when the directory is not writable.
func Stage(exe string) (string, error) {
	f, err := os.CreateTemp(filepath.Dir(exe), ".kubekaze-update-*")
	if err != nil {
		if os.IsPermission(err) {
			return "", fmt.Errorf("no permission to write in %s; run it with sudo (sudo $(which kubekaze) update) or install kubekaze in a directory you own: %w", filepath.Dir(exe), err)
		}
		return "", fmt.Errorf("creating a temporary file next to %s: %w", exe, err)
	}
	name := f.Name()
	f.Close()
	return name, nil
}

// RunSanity runs "<binary> --version" and checks that it reports want. This
// catches a wrong CPU architecture or a damaged binary before anything is replaced.
func RunSanity(path string, want Version) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "--version").CombinedOutput()
	if err != nil {
		return fmt.Errorf("running it failed: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	if !strings.Contains(string(out), want.String()) {
		return fmt.Errorf("it reports %q, expected version %s", strings.TrimSpace(string(out)), want)
	}
	return nil
}
