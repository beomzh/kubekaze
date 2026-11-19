package update

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// maxBinary caps how much is read from an archive entry.
const maxBinary = 300 << 20

// Checksums maps file names to lowercase hex SHA-256 values (checksums.txt).
type Checksums map[string]string

// ParseChecksums reads "<sha256>  <name>" lines as written by sha256sum.
func ParseChecksums(r io.Reader) (Checksums, error) {
	out := Checksums{}
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) != 2 || len(f[0]) != 64 {
			continue
		}
		out[strings.TrimPrefix(f[1], "*")] = strings.ToLower(f[0])
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, errors.New("no checksums found in the checksums file")
	}
	return out, nil
}

// SHA256File returns the hex SHA-256 of a file.
func SHA256File(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// VerifyFile checks a file against its entry in sums (by base name).
func VerifyFile(p string, sums Checksums) error {
	want, ok := sums[filepath.Base(p)]
	if !ok {
		return fmt.Errorf("%s is not listed in the checksums file", filepath.Base(p))
	}
	got, err := SHA256File(p)
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("checksum mismatch for %s: expected %s, got %s; the file is damaged or was changed", filepath.Base(p), want, got)
	}
	return nil
}

// ExtractBinary copies the kubekaze executable out of a release archive into
// dest. Only the one expected entry is read; nothing else is extracted.
func ExtractBinary(archive, goos, dest string) error {
	name := "kubekaze"
	if goos == "windows" {
		name += ".exe"
	}
	if strings.HasSuffix(archive, ".zip") {
		return extractZip(archive, name, dest)
	}
	return extractTarGz(archive, name, dest)
}

func writeLimited(dst string, r io.Reader) error {
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_TRUNC|os.O_CREATE, 0o755)
	if err != nil {
		return err
	}
	n, err := io.Copy(out, io.LimitReader(r, maxBinary+1))
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if n > maxBinary {
		return errors.New("the binary in the archive is unexpectedly large")
	}
	return nil
}

func extractTarGz(archive, name, dest string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("reading %s: %w", filepath.Base(archive), err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return fmt.Errorf("%s not found in %s", name, filepath.Base(archive))
		}
		if err != nil {
			return err
		}
		if h.Typeflag == tar.TypeReg && path.Base(h.Name) == name {
			return writeLimited(dest, tr)
		}
	}
}

func extractZip(archive, name, dest string) error {
	zr, err := zip.OpenReader(archive)
	if err != nil {
		return fmt.Errorf("reading %s: %w", filepath.Base(archive), err)
	}
	defer zr.Close()
	for _, f := range zr.File {
		if !f.FileInfo().IsDir() && path.Base(f.Name) == name {
			rc, err := f.Open()
			if err != nil {
				return err
			}
			defer rc.Close()
			return writeLimited(dest, rc)
		}
	}
	return fmt.Errorf("%s not found in %s", name, filepath.Base(archive))
}
