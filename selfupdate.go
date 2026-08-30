package tools

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// SelfUpdate implements the /dl family download contract:
//
//  1. GET <dlHost>/dl/<Name>/latest → bare semver (leading "v" tolerated).
//  2. If it equals the current version → (false, current, nil), no download.
//  3. Otherwise download <base>/<asset> and <base>/<asset>.sha256 where
//     asset = "<Name>_<goos>_<goarch>.tar.gz" and
//     base  = <dlHost>/dl/<Name>/<latest>.
//  4. Fail-closed sha256 verify against the sidecar's first field.
//  5. Extract the <Name> file from the tar.gz.
//  6. Atomically replace the running binary (stage .<Name>.new.<pid> → rename).
func (a *App) SelfUpdate(out, errw io.Writer) (updated bool, newVersion string, err error) {
	latest, err := a.fetchLatest()
	if err != nil {
		return false, "", err
	}
	current := strings.TrimPrefix(a.version.Number, "v")
	if latest == current {
		return false, current, nil
	}

	asset := fmt.Sprintf("%s_%s_%s.tar.gz", a.name, runtime.GOOS, runtime.GOARCH)
	base := fmt.Sprintf("%s/dl/%s/%s", a.dlHost, a.name, latest)

	assetBytes, err := a.download(base + "/" + asset)
	if err != nil {
		return false, "", err
	}
	sumBytes, err := a.download(base + "/" + asset + ".sha256")
	if err != nil {
		return false, "", err
	}

	want := strings.ToLower(firstField(string(sumBytes)))
	got := sha256.Sum256(assetBytes)
	if want == "" || hex.EncodeToString(got[:]) != want {
		return false, "", fmt.Errorf("checksum mismatch for %s", asset)
	}

	binBytes, err := extractBinary(assetBytes, a.name)
	if err != nil {
		return false, "", err
	}

	exe, err := a.exePath()
	if err != nil {
		return false, "", err
	}
	if resolved, rerr := filepath.EvalSymlinks(exe); rerr == nil {
		exe = resolved
	}
	if err := stageAndRename(exe, a.name, binBytes); err != nil {
		return false, "", err
	}
	return true, latest, nil
}

func (a *App) fetchLatest() (string, error) {
	body, err := a.download(fmt.Sprintf("%s/dl/%s/latest", a.dlHost, a.name))
	if err != nil {
		return "", err
	}
	return strings.TrimPrefix(strings.TrimSpace(string(body)), "v"), nil
}

func (a *App) download(url string) ([]byte, error) {
	resp, err := a.client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

// firstField returns the first whitespace-delimited field of a shasum sidecar
// line ("<hex>  <name>").
func firstField(s string) string {
	fields := strings.Fields(strings.TrimSpace(s))
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

// extractBinary decompresses the tar.gz asset and returns the bytes of the tar
// entry whose base name is bin.
func extractBinary(assetBytes []byte, bin string) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(assetBytes))
	if err != nil {
		return nil, fmt.Errorf("gzip: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("tar: %w", err)
		}
		if hdr.FileInfo().IsDir() {
			continue
		}
		if filepath.Base(hdr.Name) == bin {
			return io.ReadAll(tr)
		}
	}
	return nil, fmt.Errorf("%s not found in asset", bin)
}

// stageAndRename writes data to .<bin>.new.<pid> beside final (0755) then
// atomically renames it onto final, so a running process keeps its inode.
func stageAndRename(final, bin string, data []byte) error {
	dir := filepath.Dir(final)
	staged := filepath.Join(dir, fmt.Sprintf(".%s.new.%d", bin, os.Getpid()))
	if err := os.WriteFile(staged, data, 0o755); err != nil {
		os.Remove(staged)
		return err
	}
	// WriteFile respects umask; force mode explicitly.
	if err := os.Chmod(staged, 0o755); err != nil {
		os.Remove(staged)
		return err
	}
	if err := os.Rename(staged, final); err != nil {
		os.Remove(staged)
		return err
	}
	return nil
}
