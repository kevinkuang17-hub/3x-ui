package singbox

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/config"
)

// Version is the sing-box release the panel fetches on first use; AnyTLS needs 1.12 or later.
const Version = "1.14.3"

// BinaryName follows the Xray and mtg naming in the bin folder.
func BinaryName() string {
	name := fmt.Sprintf("sing-box-%s-%s", runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return name
}

func BinaryPath() string {
	return filepath.Join(config.GetBinFolderPath(), BinaryName())
}

// archiveName is the release asset for this platform, or "" where sing-box publishes none.
func archiveName(goos, goarch string) string {
	arch := goarch
	switch goarch {
	case "amd64", "arm64", "386", "s390x", "riscv64", "loong64":
	case "arm":
		arch = "armv7"
	default:
		return ""
	}
	switch goos {
	case "linux":
		return fmt.Sprintf("sing-box-%s-linux-%s.tar.gz", Version, arch)
	case "windows":
		if goarch == "amd64" || goarch == "arm64" || goarch == "386" {
			return fmt.Sprintf("sing-box-%s-windows-%s.zip", Version, arch)
		}
	}
	return ""
}

// fetch downloads a release asset; tests replace it.
var fetch = func(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 200<<20))
}

// EnsureBinary downloads sing-box into the bin folder unless it is already there, so panels that
// never use AnyTLS or NaiveProxy carry no extra binary.
func EnsureBinary(ctx context.Context) error {
	dst := BinaryPath()
	if _, err := os.Stat(dst); err == nil {
		return nil
	}
	asset := archiveName(runtime.GOOS, runtime.GOARCH)
	if asset == "" {
		return fmt.Errorf("sing-box has no release for %s/%s; place a binary at %s", runtime.GOOS, runtime.GOARCH, dst)
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	url := fmt.Sprintf("https://github.com/SagerNet/sing-box/releases/download/v%s/%s", Version, asset)
	data, err := fetch(ctx, url)
	if err != nil {
		return fmt.Errorf("downloading sing-box: %w", err)
	}
	bin, err := extractBinary(asset, data)
	if err != nil {
		return err
	}
	tmp := dst + ".download"
	if err := os.WriteFile(tmp, bin, 0o755); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

var errNoBinary = errors.New("sing-box binary not found in the release archive")

func extractBinary(asset string, data []byte) ([]byte, error) {
	if path.Ext(asset) == ".zip" {
		zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return nil, err
		}
		for _, f := range zr.File {
			if path.Base(f.Name) == "sing-box.exe" {
				rc, err := f.Open()
				if err != nil {
					return nil, err
				}
				defer rc.Close()
				return io.ReadAll(rc)
			}
		}
		return nil, errNoBinary
	}
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil, errNoBinary
		}
		if err != nil {
			return nil, err
		}
		if hdr.Typeflag == tar.TypeReg && path.Base(hdr.Name) == "sing-box" {
			return io.ReadAll(tr)
		}
	}
}
