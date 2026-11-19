// Package update implements "kubekaze update": finding a newer release,
// downloading it (or taking an archive you brought into an offline network),
// verifying its checksum and replacing the running binary safely.
package update

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Version is a semantic version without build metadata.
type Version struct {
	Major, Minor, Patch int
	Pre                 string // e.g. "rc.1"; empty for a release
}

var versionRE = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)(?:-([0-9A-Za-z.-]+))?(?:\+[0-9A-Za-z.-]+)?$`)

// ParseVersion accepts "v0.1.1", "0.1.1" and "0.1.1-rc.1".
func ParseVersion(s string) (Version, error) {
	m := versionRE.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return Version{}, fmt.Errorf("%q is not a version like v1.2.3", s)
	}
	n := func(i int) int { v, _ := strconv.Atoi(m[i]); return v }
	return Version{n(1), n(2), n(3), m[4]}, nil
}

// String returns the version without a leading "v".
func (v Version) String() string {
	s := fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
	if v.Pre != "" {
		s += "-" + v.Pre
	}
	return s
}

// Tag returns the git tag form, "v1.2.3".
func (v Version) Tag() string { return "v" + v.String() }

// Compare returns -1, 0 or 1. A pre-release is older than its release.
func (v Version) Compare(o Version) int {
	for _, p := range [][2]int{{v.Major, o.Major}, {v.Minor, o.Minor}, {v.Patch, o.Patch}} {
		if p[0] != p[1] {
			if p[0] < p[1] {
				return -1
			}
			return 1
		}
	}
	switch {
	case v.Pre == o.Pre:
		return 0
	case v.Pre == "":
		return 1
	case o.Pre == "":
		return -1
	case v.Pre < o.Pre:
		return -1
	}
	return 1
}

var tagRE = regexp.MustCompile(`^v\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$`)

// ValidTag reports whether s is safe to use in a URL path as a release tag.
func ValidTag(s string) bool { return tagRE.MatchString(s) }

// ArchiveName is the release archive for a version and platform.
func ArchiveName(v Version, goos, goarch string) string {
	ext := "tar.gz"
	if goos == "windows" {
		ext = "zip"
	}
	return fmt.Sprintf("kubekaze_%s_%s_%s.%s", v.String(), goos, goarch, ext)
}

var archiveRE = regexp.MustCompile(`^kubekaze_(.+)_(linux|darwin|windows)_(amd64|arm64)\.(tar\.gz|zip)$`)

// ParseArchiveName splits a release archive file name into its parts.
func ParseArchiveName(name string) (v Version, goos, goarch string, err error) {
	m := archiveRE.FindStringSubmatch(name)
	if m == nil {
		return Version{}, "", "", fmt.Errorf("%q does not look like a kubekaze release archive (kubekaze_<version>_<os>_<arch>.tar.gz|zip)", name)
	}
	v, err = ParseVersion(m[1])
	if err != nil {
		return Version{}, "", "", err
	}
	return v, m[2], m[3], nil
}
