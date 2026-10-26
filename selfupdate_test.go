package tools

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/subaud-tools/tools-common/auth"
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
		_, _ = fmt.Fprintln(w, latest)
	})
	mux.HandleFunc("/dl/"+name+"/"+latest+"/"+asset, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(tarball)
	})
	mux.HandleFunc("/dl/"+name+"/"+latest+"/"+asset+".sha256", func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, sidecar)
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
	_ = os.WriteFile(exe, []byte("old"), 0o755)
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
	_ = os.WriteFile(exe, []byte("old"), 0o755)
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
	_ = os.WriteFile(exe, []byte("old"), 0o755)
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
	_ = os.WriteFile(exe, []byte("old"), 0o755)
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

// linksServer stands for the access service's POST /links: status 200
// answers a signed URL on files, else the service's error body. It counts
// the paths it was asked for.
func linksServer(t *testing.T, files *httptest.Server, status int) (*httptest.Server, *[]string) {
	t.Helper()
	var asked []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Product, Path string }
		_ = json.NewDecoder(r.Body).Decode(&in)
		if r.Header.Get("Authorization") != "Bearer sbt_T" || in.Product != "kempt" {
			t.Errorf("asked by %q for %q", r.Header.Get("Authorization"), in.Product)
		}
		asked = append(asked, in.Path)
		w.WriteHeader(status)
		switch status {
		case http.StatusOK:
			_ = json.NewEncoder(w).Encode(map[string]string{"url": files.URL + "/dl/" + in.Path + "?Signature=S"})
		case http.StatusUnauthorized:
			_, _ = io.WriteString(w, `{"error": "sign_in", "account": "https://subaud.tools/account/"}`)
		case http.StatusForbidden:
			_, _ = io.WriteString(w, `{"error": "not_owned", "account": "https://subaud.tools/account/"}`)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &asked
}

// gatedApp is a kempt App whose downloads go through links, with a token
// on the machine when signedIn. Its own /dl host is a server that fails
// the test when reached.
func gatedApp(t *testing.T, gated bool, links *httptest.Server, signedIn bool, exe string) *App {
	t.Helper()
	t.Setenv(auth.TokenEnv, "")
	a := New(Config{Name: "kempt", Domain: "kempt.tools", Version: Version{Number: "0.1.0"}, Gated: gated})
	tokenPath := filepath.Join(t.TempDir(), "token")
	if signedIn {
		if err := os.WriteFile(tokenPath, []byte("sbt_T\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	a.auth = &auth.Client{API: links.URL, Path: tokenPath}
	a.exePath = func() (string, error) { return exe, nil }
	return a
}

func TestAGatedToolUpdatesThroughSignedLinks(t *testing.T) {
	newBin := []byte("gated binary")
	tarball := makeTarGz(t, "kempt", newBin)
	files := dlServer(t, "kempt", "0.2.0", tarball, sidecarFor("kempt", tarball))
	defer files.Close()
	links, asked := linksServer(t, files, http.StatusOK)
	exe := filepath.Join(t.TempDir(), "kempt")
	_ = os.WriteFile(exe, []byte("old"), 0o755)
	a := gatedApp(t, true, links, true, exe)
	a.setDLHost("http://127.0.0.1:1") // never reached: every URL is a signed link

	updated, ver, err := a.SelfUpdate(io.Discard, io.Discard)
	if err != nil || !updated || ver != "0.2.0" {
		t.Fatalf("updated %v, ver %q, err %v", updated, ver, err)
	}
	if b, _ := os.ReadFile(exe); string(b) != string(newBin) {
		t.Errorf("target not replaced: %q", b)
	}
	want := []string{"kempt/latest", "kempt/0.2.0/" + assetName("kempt"), "kempt/0.2.0/" + assetName("kempt") + ".sha256"}
	if fmt.Sprint(*asked) != fmt.Sprint(want) {
		t.Fatalf("asked %v, want %v", *asked, want)
	}
}

func TestAGatedToolWithNoTokenNamesLogin(t *testing.T) {
	files := dlServer(t, "kempt", "0.2.0", nil, "")
	defer files.Close()
	links, asked := linksServer(t, files, http.StatusOK)
	a := gatedApp(t, true, links, false, filepath.Join(t.TempDir(), "kempt"))
	var out, errw bytes.Buffer
	if code := a.Dispatch([]string{"update"}, &out, &errw); code != 1 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(errw.String(), "run `kempt login`") || len(*asked) != 0 {
		t.Fatalf("errw %q, asked %v", errw.String(), *asked)
	}
}

func TestAGatedToolWithARefusedTokenNamesLogin(t *testing.T) {
	files := dlServer(t, "kempt", "0.2.0", nil, "")
	defer files.Close()
	links, _ := linksServer(t, files, http.StatusUnauthorized)
	a := gatedApp(t, true, links, true, filepath.Join(t.TempDir(), "kempt"))
	_, _, err := a.SelfUpdate(io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "run `kempt login`") {
		t.Fatalf("err = %v", err)
	}
}

func TestAGatedToolNotOwnedNamesTheAccountPage(t *testing.T) {
	files := dlServer(t, "kempt", "0.2.0", nil, "")
	defer files.Close()
	links, _ := linksServer(t, files, http.StatusForbidden)
	a := gatedApp(t, true, links, true, filepath.Join(t.TempDir(), "kempt"))
	_, _, err := a.SelfUpdate(io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "https://subaud.tools/account/") {
		t.Fatalf("err = %v", err)
	}
}

func TestAnUngatedToolNeverAsksForLinks(t *testing.T) {
	tarball := makeTarGz(t, "kempt", []byte("plain binary"))
	files := dlServer(t, "kempt", "0.2.0", tarball, sidecarFor("kempt", tarball))
	defer files.Close()
	links, asked := linksServer(t, files, http.StatusOK)
	exe := filepath.Join(t.TempDir(), "kempt")
	_ = os.WriteFile(exe, []byte("old"), 0o755)
	a := gatedApp(t, false, links, true, exe)
	a.setDLHost(files.URL)

	if updated, _, err := a.SelfUpdate(io.Discard, io.Discard); err != nil || !updated {
		t.Fatalf("updated %v, err %v", updated, err)
	}
	if len(*asked) != 0 {
		t.Fatalf("asked for links: %v", *asked)
	}
}

// A signed link's query is a credential for five minutes: a failed
// connection must not print it.
func TestAFailedDownloadNamesTheFileWithoutTheSignature(t *testing.T) {
	a := New(Config{Name: "kempt", Domain: "kempt.tools", Version: Version{Number: "0.1.0"}})
	_, err := a.download("http://127.0.0.1:1/dl/kempt/latest?Expires=1&Signature=SECRET&Key-Pair-Id=K")
	if err == nil {
		t.Fatal("no error")
	}
	if strings.Contains(err.Error(), "SECRET") || strings.Contains(err.Error(), "Signature") {
		t.Fatalf("the error carries the signature: %v", err)
	}
	if !strings.Contains(err.Error(), "/dl/kempt/latest") {
		t.Fatalf("the error should name the file: %v", err)
	}
}
