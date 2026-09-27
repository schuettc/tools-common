package page

import (
	"context"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/schuettc/tools-common/localweb"
)

// manifest is every file the kit ships; each task that adds an asset adds it here.
var manifest = []string{"kit.css", "kit.js", "kit.d.ts", "boot.js", "theme.js", "keys.js"}

func TestFSEmbedsAssets(t *testing.T) {
	for _, name := range manifest {
		if _, err := fs.ReadFile(FS(), name); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func get(t *testing.T, url string) (int, string, string) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b), resp.Header.Get("Content-Type")
}

func toolFS() fstest.MapFS {
	return fstest.MapFS{
		"index.html":  {Data: []byte("page")},
		"_kit/kit.js": {Data: []byte("evil")},
	}
}

func TestWithOverlay(t *testing.T) {
	srv := httptest.NewServer(http.FileServerFS(With(toolFS())))
	defer srv.Close()

	if code, body, _ := get(t, srv.URL+"/index.html"); code != 200 || body != "page" {
		t.Errorf("index: %d %q", code, body)
	}
	want, _ := fs.ReadFile(FS(), "kit.js")
	if code, body, _ := get(t, srv.URL+"/_kit/kit.js"); code != 200 || body != string(want) {
		t.Errorf("kit.js: %d %q, want the embedded file", code, body)
	}
	if code, _, _ := get(t, srv.URL+"/_kit/nope.js"); code != 404 {
		t.Errorf("missing kit file: %d, want 404", code)
	}
}

func TestWithNilTool(t *testing.T) {
	srv := httptest.NewServer(http.FileServerFS(With(nil)))
	defer srv.Close()
	if code, _, _ := get(t, srv.URL+"/_kit/kit.css"); code != 200 {
		t.Errorf("kit.css: %d", code)
	}
	if code, _, _ := get(t, srv.URL+"/index.html"); code != 404 {
		t.Errorf("index with nil tool: %d, want 404", code)
	}
}

func TestWithReadDir(t *testing.T) {
	root, err := fs.ReadDir(With(toolFS()), ".")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range root {
		names = append(names, e.Name())
		if e.Name() == Prefix && !e.IsDir() {
			t.Errorf("%s is not a dir", Prefix)
		}
	}
	if len(names) != 2 || names[0] != Prefix || names[1] != "index.html" {
		t.Errorf("root entries = %v, want [_kit index.html]", names)
	}
	kitDir, err := fs.ReadDir(With(toolFS()), Prefix)
	if err != nil {
		t.Fatal(err)
	}
	direct, _ := fs.ReadDir(FS(), ".")
	if len(kitDir) != len(direct) {
		t.Fatalf("_kit has %d entries, FS has %d", len(kitDir), len(direct))
	}
	for i := range direct {
		if kitDir[i].Name() != direct[i].Name() {
			t.Errorf("entry %d: %s vs %s", i, kitDir[i].Name(), direct[i].Name())
		}
	}
}

func TestServedThroughLocalweb(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	s, err := localweb.Start(ctx, localweb.Config{Tool: "pagetest", Assets: With(toolFS())})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); _ = s.Wait() })

	for path, ct := range map[string]string{
		"/_kit/kit.js":  "text/javascript; charset=utf-8",
		"/_kit/kit.css": "text/css; charset=utf-8",
	} {
		code, _, got := get(t, "http://"+s.Addr()+path)
		if code != 200 || got != ct {
			t.Errorf("%s: %d %q, want 200 %q", path, code, got, ct)
		}
	}
}
