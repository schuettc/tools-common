package page

import (
	"context"
	"html"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// The kit's JS is tested in headless Chrome: each testdata/<name>.test.js
// suite runs under testdata/run.html, served with the kit at /_kit/ exactly as
// a tool's page is. Without Chrome the tests skip, unless KIT_BROWSER=required
// (CI), where a missing browser is a failure.

func chromePath() string {
	if p := os.Getenv("KIT_CHROME"); p != "" {
		return p
	}
	for _, name := range []string{"chrome-headless-shell", "google-chrome", "google-chrome-stable", "chromium", "chromium-browser"} {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	mac := "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
	if _, err := os.Stat(mac); err == nil {
		return mac
	}
	return ""
}

func needChrome(t *testing.T) string {
	t.Helper()
	p := chromePath()
	if p == "" {
		if os.Getenv("KIT_BROWSER") == "required" {
			t.Fatal("KIT_BROWSER=required but no Chrome found (set KIT_CHROME)")
		}
		t.Skip("no Chrome; set KIT_CHROME to run the kit's JS tests")
	}
	return p
}

// dumpDOM loads url in headless Chrome and returns the DOM after scripts ran.
// It stops reading at </html> and kills the browser: full Chrome on macOS
// leaves an updater child holding stdout open long after the dump.
func dumpDOM(t *testing.T, url string) (string, error) {
	t.Helper()
	chrome := needChrome(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	args := []string{"--disable-gpu", "--no-first-run", "--no-default-browser-check",
		"--user-data-dir=" + t.TempDir(), "--virtual-time-budget=15000", "--dump-dom", url}
	if !strings.Contains(filepath.Base(chrome), "headless-shell") {
		args = append([]string{"--headless=new"}, args...)
	}
	cmd := exec.CommandContext(ctx, chrome, args...)
	cmd.WaitDelay = time.Second
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	if err := cmd.Start(); err != nil {
		return "", err
	}
	var buf strings.Builder
	chunk := make([]byte, 32<<10)
	for !strings.Contains(buf.String(), "</html>") {
		n, err := stdout.Read(chunk)
		buf.Write(chunk[:n])
		if err != nil {
			break
		}
	}
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
	if ctx.Err() != nil {
		return buf.String(), ctx.Err()
	}
	return buf.String(), nil
}

var resultRE = regexp.MustCompile(`(?s)<pre id="result">(.*?)</pre>`)

func serveTestdata(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.FileServerFS(With(os.DirFS("testdata"))))
	t.Cleanup(srv.Close)
	return srv
}

// browserResult runs one suite and returns its result text.
func browserResult(t *testing.T, name string) (string, error) {
	t.Helper()
	srv := serveTestdata(t)
	dom, err := dumpDOM(t, srv.URL+"/run.html?t="+name)
	if err != nil {
		return "", err
	}
	m := resultRE.FindStringSubmatch(dom)
	if m == nil {
		return "", &noResult{dom}
	}
	return html.UnescapeString(m[1]), nil
}

type noResult struct{ dom string }

func (e *noResult) Error() string { return "no result in page:\n" + e.dom }

func runBrowser(t *testing.T, name string) {
	t.Helper()
	res, err := browserResult(t, name)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(res, "PASS ") {
		t.Fatalf("%s:\n%s", name, res)
	}
	t.Log(strings.SplitN(res, "\n", 2)[0])
}

func TestJSSmoke(t *testing.T) { runBrowser(t, "smoke") }

func TestHarnessReportsFailure(t *testing.T) {
	res, err := browserResult(t, "fail")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(res, "FAIL 1") || !strings.Contains(res, "deliberate") {
		t.Fatalf("want a reported failure, got %q", res)
	}
}
