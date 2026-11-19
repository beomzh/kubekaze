package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/beomzh/kubekaze/internal/term"
	"github.com/beomzh/kubekaze/internal/ui"
	"github.com/beomzh/kubekaze/internal/update"
	"github.com/beomzh/kubekaze/internal/version"
)

type updateFlags struct {
	check     bool
	version   string
	url       string
	from      string
	checksums string
	yes       bool
	force     bool
}

func (a *app) newUpdateCmd() *cobra.Command {
	var f updateFlags
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update kubekaze to the latest release (online, from a mirror, or from a file)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if f.url == "" {
				f.url = os.Getenv("KUBEKAZE_UPDATE_URL")
			}
			return a.runUpdate(cmd.Context(), f)
		},
	}
	fl := cmd.Flags()
	fl.BoolVar(&f.check, "check", false, "only say whether a newer version exists")
	fl.StringVar(&f.version, "version", "", "install this version (for example v0.1.1) instead of the latest")
	fl.StringVar(&f.url, "url", "", "download from this mirror instead of GitHub (or set KUBEKAZE_UPDATE_URL)")
	fl.StringVar(&f.from, "from", "", "install from a release archive you already downloaded (no network needed)")
	fl.StringVar(&f.checksums, "checksums", "", "with --from: the checksums.txt file (default: the one next to the archive)")
	fl.BoolVarP(&f.yes, "yes", "y", false, "do not ask for confirmation")
	fl.BoolVar(&f.force, "force", false, "reinstall the same or an older version, or replace a development build")
	return cmd
}

// exePath returns the running binary with symlinks resolved.
func (a *app) exePath() (string, error) {
	if a.exe != nil {
		return a.exe()
	}
	p, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(p)
}

func (a *app) runUpdate(ctx context.Context, f updateFlags) error {
	exe, err := a.exePath()
	if err != nil {
		return fmt.Errorf("finding the running kubekaze: %w", err)
	}
	cur, curErr := update.ParseVersion(version.Version)
	curLabel := version.Version
	if curErr == nil {
		curLabel = cur.Tag()
	}
	u := &update.Updater{
		Client: update.NewClient(f.url, "kubekaze/"+version.Version),
		Exe:    exe, GOOS: runtime.GOOS, GOARCH: runtime.GOARCH, Sanity: a.sanity,
	}

	// ---- offline: a downloaded archive ----
	if f.from != "" {
		if f.check {
			return fmt.Errorf("--check and --from cannot be combined")
		}
		v, _, _, err := update.ParseArchiveName(filepath.Base(f.from))
		if err != nil {
			return err
		}
		sums := f.checksums
		if sums == "" {
			sums = filepath.Join(filepath.Dir(f.from), "checksums.txt")
		}
		if _, err := os.Stat(sums); err != nil {
			return fmt.Errorf("no checksums file found at %s; download checksums.txt from the same release and put it next to the archive, or pass --checksums", sums)
		}
		if err := a.guard(exe, curErr, curLabel, f); err != nil {
			return err
		}
		if curErr == nil && v.Compare(cur) <= 0 && !f.force {
			fmt.Fprintf(a.out, "Nothing to do: the archive is %s and you have %s. Use --force to install it anyway.\n", v.Tag(), curLabel)
			return nil
		}
		if err := a.confirmUpdate(curLabel, v.Tag(), exe, f); err != nil {
			return err
		}
		got, err := u.ApplyArchive(f.from, sums)
		if err != nil {
			return err
		}
		a.updated(curLabel, got, f)
		return nil
	}

	// ---- online or mirror ----
	tag := f.version
	if tag == "" {
		tag, err = u.Client.Latest(ctx)
		if err != nil {
			return fmt.Errorf("%w\n\nNo internet access? Download the archive and checksums.txt for your platform elsewhere, copy them here, and run:\n  kubekaze update --from kubekaze_<version>_%s_%s.%s",
				err, runtime.GOOS, runtime.GOARCH, map[bool]string{true: "zip", false: "tar.gz"}[runtime.GOOS == "windows"])
		}
	} else if !update.ValidTag(tag) {
		if v, perr := update.ParseVersion(tag); perr == nil {
			tag = v.Tag()
		} else {
			return fmt.Errorf("invalid --version %q (example: v0.1.1)", f.version)
		}
	}
	target, _ := update.ParseVersion(tag)
	available := curErr != nil || target.Compare(cur) > 0

	if f.check {
		if a.output == "json" {
			enc := json.NewEncoder(a.out)
			enc.SetIndent("", "  ")
			return enc.Encode(map[string]any{"current": curLabel, "latest": tag, "updateAvailable": available})
		}
		if available {
			fmt.Fprintf(a.out, "A newer version is available: %s (you have %s). Run: kubekaze update\n", tag, curLabel)
		} else {
			fmt.Fprintf(a.out, "%s Up to date (%s).\n", a.pal.Green("OK"), curLabel)
		}
		return nil
	}
	if err := a.guard(exe, curErr, curLabel, f); err != nil {
		return err
	}
	if !available && !f.force {
		fmt.Fprintf(a.out, "%s Already up to date (%s).\n", a.pal.Green("OK"), curLabel)
		return nil
	}
	if err := a.confirmUpdate(curLabel, tag, exe, f); err != nil {
		return err
	}
	fmt.Fprintln(a.err, a.pal.Dim("Downloading "+tag+"..."))
	got, err := u.DownloadAndApply(ctx, tag)
	if err != nil {
		return fmt.Errorf("%w\n\nNo internet access? Use: kubekaze update --from <archive> (see: kubekaze update --help)", err)
	}
	a.updated(curLabel, got, f)
	return nil
}

// guard refuses updates that would do harm.
func (a *app) guard(exe string, curErr error, curLabel string, f updateFlags) error {
	if m := update.ManagedBy(exe); m != "" && !f.force {
		return fmt.Errorf("this kubekaze was installed by a package manager (%s). Use --force to replace it anyway", m)
	}
	if curErr != nil && !f.force {
		return fmt.Errorf("this is a development build (version %q); use --force to replace it with a release", curLabel)
	}
	return nil
}

func (a *app) confirmUpdate(cur, target, exe string, f updateFlags) error {
	if f.yes {
		return nil
	}
	if !a.interactive {
		return fmt.Errorf("not a terminal: pass --yes to update without asking")
	}
	if !a.confirmYN(fmt.Sprintf("Update %s -> %s in %s? [y/N]: ", cur, target, term.SafeText(exe))) {
		fmt.Fprintln(a.err, "Aborted. Nothing changed.")
		return ui.ErrCancelled
	}
	return nil
}

func (a *app) updated(cur string, v update.Version, f updateFlags) {
	fmt.Fprintf(a.out, "%s Updated kubekaze %s -> %s\n", a.pal.Green("OK"), cur, v.Tag())
	if f.url == "" && f.from == "" {
		fmt.Fprintf(a.out, "Release notes: https://github.com/%s/releases/tag/%s\n", update.Repo, v.Tag())
	}
}
