package decoy

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const (
	maxSiteFiles = 2000
	maxSiteBytes = 50 << 20
)

// InstallZip replaces the custom site in dir with the archive's files. An archive whose files all
// sit in one top folder is unpacked from inside it, the way most site downloads are packed.
func InstallZip(r io.ReaderAt, size int64, dir string) (int, error) {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return 0, errors.New("not a zip archive")
	}
	var files []*zip.File
	var total uint64
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		if !f.Mode().IsRegular() {
			return 0, fmt.Errorf("%s: only regular files are allowed", f.Name)
		}
		total += f.UncompressedSize64
		files = append(files, f)
	}
	if len(files) == 0 {
		return 0, errors.New("the archive is empty")
	}
	if len(files) > maxSiteFiles || total > maxSiteBytes {
		return 0, fmt.Errorf("the site may hold at most %d files and %d MB", maxSiteFiles, maxSiteBytes>>20)
	}
	names := make([]string, len(files))
	for i, f := range files {
		name, err := cleanEntry(f.Name)
		if err != nil {
			return 0, err
		}
		names[i] = name
	}
	prefix := commonTopFolder(names)
	hasIndex := false
	for i := range names {
		names[i] = strings.TrimPrefix(names[i], prefix)
		if names[i] == "index.html" {
			hasIndex = true
		}
	}
	if !hasIndex {
		return 0, errors.New("the archive needs an index.html at its top level")
	}

	staging := dir + ".new"
	if err := os.RemoveAll(staging); err != nil {
		return 0, err
	}
	for i, f := range files {
		if err := extractFile(f, filepath.Join(staging, filepath.FromSlash(names[i]))); err != nil {
			os.RemoveAll(staging)
			return 0, err
		}
	}
	if err := os.RemoveAll(dir); err != nil {
		os.RemoveAll(staging)
		return 0, err
	}
	if err := os.Rename(staging, dir); err != nil {
		return 0, err
	}
	return len(files), nil
}

// cleanEntry rejects names that would land outside the site folder (zip slip).
func cleanEntry(name string) (string, error) {
	name = strings.ReplaceAll(name, "\\", "/")
	clean := path.Clean("/" + name)
	if clean == "/" || strings.Contains(name, "..") || strings.HasPrefix(name, "/") || strings.Contains(name, ":") {
		return "", fmt.Errorf("unsafe file name in archive: %q", name)
	}
	return strings.TrimPrefix(clean, "/"), nil
}

func commonTopFolder(names []string) string {
	first, _, found := strings.Cut(names[0], "/")
	if !found {
		return ""
	}
	prefix := first + "/"
	for _, n := range names[1:] {
		if !strings.HasPrefix(n, prefix) {
			return ""
		}
	}
	return prefix
}

func extractFile(f *zip.File, dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	src, err := f.Open()
	if err != nil {
		return err
	}
	defer src.Close()
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	// Sizes were checked from the headers; the limit keeps an archive that lies about them in bounds.
	_, copyErr := io.CopyN(out, src, int64(f.UncompressedSize64))
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}
