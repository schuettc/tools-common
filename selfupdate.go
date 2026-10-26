package tools

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/subaud-tools/tools-common/auth"
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
//
// A gated tool fetches each of the three files through a signed link the
// access service gives this machine's token (auth.Link), instead of from
// <dlHost>/dl.
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
	base := a.name + "/" + latest

	assetBytes, err := a.fetch(base + "/" + asset)
	if err != nil {
		return false, "", err
	}
	sumBytes, err := a.fetch(base + "/" + asset + ".sha256")
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
	body, err := a.fetch(a.name + "/latest")
	if err != nil {
		return "", err
	}
	return strings.TrimPrefix(strings.TrimSpace(string(body)), "v"), nil
}

// fetch downloads path under /dl: through a signed link for a gated tool,
// else from <dlHost>/dl/<path>.
func (a *App) fetch(path string) ([]byte, error) {
	if !a.gated {
		return a.download(a.dlHost + "/dl/" + path)
	}
	link, err := a.auth.Link(context.Background(), a.name, path)
	switch {
	case errors.Is(err, auth.ErrNotSignedIn):
		return nil, fmt.Errorf("this machine is not signed in to subaud.tools: run `%s login`", a.name)
	case errors.Is(err, auth.ErrSignInRefused):
		return nil, fmt.Errorf("subaud.tools refused this machine's sign-in: run `%s login` to sign in again", a.name)
	case err != nil:
		return nil, err
	}
	return a.download(link)
}

func (a *App) download(url string) ([]byte, error) {
	// A signed link's query is its signature: name the file without it.
	file := strings.SplitN(url, "?", 2)[0]
	resp, err := a.client.Get(url)
	if err != nil {
		var ue *neturl.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return nil, fmt.Errorf("GET %s: %w", file, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", file, resp.Status)
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
	defer func() { _ = gz.Close() }()
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
		_ = os.Remove(staged)
		return err
	}
	// WriteFile respects umask; force mode explicitly.
	if err := os.Chmod(staged, 0o755); err != nil {
		_ = os.Remove(staged)
		return err
	}
	if err := os.Rename(staged, final); err != nil {
		_ = os.Remove(staged)
		return err
	}
	return nil
}
