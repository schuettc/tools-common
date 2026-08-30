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
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// makeTarGz builds a gzip+tar containing a single file named bin with content.
func makeTarGz(t *testing.T, bin string, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	hdr := &tar.Header{Name: bin, Mode: 0o755, Size: int64(len(content))}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func assetName(name string) string {
	return fmt.Sprintf("%s_%s_%s.tar.gz", name, runtime.GOOS, runtime.GOARCH)
}

// dlServer returns an httptest.Server serving the /dl contract.
func dlServer(t *testing.T, name, latest string, tarball []byte, sidecar string) *httptest.Server {
	t.Helper()
	asset := assetName(name)
	mux := http.NewServeMux()
	mux.HandleFunc("/dl/"+name+"/latest", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, latest)
	})
	mux.HandleFunc("/dl/"+name+"/"+latest+"/"+asset, func(w http.ResponseWriter, r *http.Request) {
		w.Write(tarball)
	})
	mux.HandleFunc("/dl/"+name+"/"+latest+"/"+asset+".sha256", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, sidecar)
	})
	return httptest.NewServer(mux)
}

func newUpdateApp(t *testing.T, srv *httptest.Server, current, exe string) *App {
	a := New(Config{Name: "kempt", Domain: "kempt.tools", Version: Version{Number: current}})
	a.setDLHost(srv.URL)
	a.exePath = func() (string, error) { return exe, nil }
	return a
}

func sidecarFor(name string, tarball []byte) string {
	sum := sha256.Sum256(tarball)
	return fmt.Sprintf("%s  %s\n", hex.EncodeToString(sum[:]), assetName(name))
}

func TestSelfUpdateNoop(t *testing.T) {
	tarball := makeTarGz(t, "kempt", []byte("new binary"))
	srv := dlServer(t, "kempt", "0.1.0", tarball, sidecarFor("kempt", tarball))
	defer srv.Close()

	exe := filepath.Join(t.TempDir(), "kempt")
	os.WriteFile(exe, []byte("old"), 0o755)
	a := newUpdateApp(t, srv, "0.1.0", exe)

	updated, ver, err := a.SelfUpdate(io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if updated {
		t.Errorf("updated = true, want false")
	}
	if ver != "0.1.0" {
		t.Errorf("ver = %q, want 0.1.0", ver)
	}
	if b, _ := os.ReadFile(exe); string(b) != "old" {
		t.Errorf("target changed: %q", b)
	}
}

func TestSelfUpdateSuccess(t *testing.T) {
	newBin := []byte("brand new binary bytes")
	tarball := makeTarGz(t, "kempt", newBin)
	srv := dlServer(t, "kempt", "0.2.0", tarball, sidecarFor("kempt", tarball))
	defer srv.Close()

	exe := filepath.Join(t.TempDir(), "kempt")
	os.WriteFile(exe, []byte("old"), 0o755)
	a := newUpdateApp(t, srv, "0.1.0", exe)

	updated, ver, err := a.SelfUpdate(io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if !updated {
		t.Errorf("updated = false, want true")
	}
	if ver != "0.2.0" {
		t.Errorf("ver = %q, want 0.2.0", ver)
	}
	if b, _ := os.ReadFile(exe); string(b) != string(newBin) {
		t.Errorf("target not replaced: %q", b)
	}
}

func TestSelfUpdateBadChecksum(t *testing.T) {
	tarball := makeTarGz(t, "kempt", []byte("new binary"))
	badSidecar := fmt.Sprintf("%s  %s\n", "deadbeef", assetName("kempt"))
	srv := dlServer(t, "kempt", "0.2.0", tarball, badSidecar)
	defer srv.Close()

	exe := filepath.Join(t.TempDir(), "kempt")
	os.WriteFile(exe, []byte("old"), 0o755)
	a := newUpdateApp(t, srv, "0.1.0", exe)

	updated, _, err := a.SelfUpdate(io.Discard, io.Discard)
	if err == nil {
		t.Fatal("expected checksum error, got nil")
	}
	if updated {
		t.Errorf("updated = true, want false")
	}
	if b, _ := os.ReadFile(exe); string(b) != "old" {
		t.Errorf("target changed on bad checksum: %q", b)
	}
}

func TestSelfUpdateMissingBinary(t *testing.T) {
	tarball := makeTarGz(t, "notkempt", []byte("new binary"))
	srv := dlServer(t, "kempt", "0.2.0", tarball, sidecarFor("kempt", tarball))
	defer srv.Close()

	exe := filepath.Join(t.TempDir(), "kempt")
	os.WriteFile(exe, []byte("old"), 0o755)
	a := newUpdateApp(t, srv, "0.1.0", exe)

	updated, _, err := a.SelfUpdate(io.Discard, io.Discard)
	if err == nil {
		t.Fatal("expected missing-binary error, got nil")
	}
	if updated {
		t.Errorf("updated = true, want false")
	}
	if b, _ := os.ReadFile(exe); string(b) != "old" {
		t.Errorf("target changed: %q", b)
	}
}
