package pageserve

// Moved from cull's internal/cull/serve/serve_test.go: Run with a stand-in
// page and API where cull served its review over its store.

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/subaud-tools/tools-common/localweb"
)

type running struct {
	done    chan error
	url     chan string
	log     *strings.Builder
	started chan string // OnStart's url
}

var testPage = fstest.MapFS{"index.html": {Data: []byte("<!doctype html><title>t</title>")}}

// testAPI is the tool's own route: GET /api/ping.
func testAPI() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/ping", func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "pong") })
	return mux
}

func startRun(t *testing.T, o Options) *running {
	t.Helper()
	t.Setenv("CULL_HOME", t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	r := &running{done: make(chan error, 1), url: make(chan string, 1), log: &strings.Builder{}, started: make(chan string, 1)}
	o.Live, o.Assets, o.API = LiveFor("cull"), testPage, testAPI()
	o.Log = &lockedWriter{w: r.log}
	o.OnStart = func(_ context.Context, u string) { r.started <- u }
	o.Ready = func(u string) { r.url <- u }
	go func() { r.done <- Run(ctx, o) }()
	select {
	case <-r.url:
	case err := <-r.done:
		t.Fatalf("run exited: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("not ready")
	}
	return r
}

type lockedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

func wait(t *testing.T, r *running) {
	t.Helper()
	select {
	case err := <-r.done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("did not exit")
	}
}

func get(t *testing.T, e Entry, path string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest("GET", e.Base+path, nil)
	req.Header.Set(localweb.TokenHeader, e.Token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestAdvertPrivateAndRemovedOnExit(t *testing.T) {
	r := startRun(t, Options{Version: "v"})
	live := LiveFor("cull")
	st, err := os.Stat(live.ServePath())
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("advert %v", st.Mode().Perm())
	}
	if d, _ := os.Stat(live.Dir); d.Mode().Perm() != 0o700 {
		t.Fatalf("dir %v", d.Mode().Perm())
	}
	e, err := live.Running()
	if err != nil || e.PID != os.Getpid() || e.Version != "v" || e.Token == "" {
		t.Fatalf("%+v %v", e, err)
	}
	if u := <-r.started; u != e.URL {
		t.Fatalf("OnStart got %q, want %q", u, e.URL)
	}
	// a second serve is refused while this one lives
	if err := Run(context.Background(), Options{Live: live}); err == nil || err.Error() != "cull serve is already running (pid "+strconv.Itoa(e.PID)+", "+e.Base+")" {
		t.Fatalf("second run: %v", err)
	}
	if err := Stop(context.Background(), live); err != nil {
		t.Fatal(err)
	}
	wait(t, r)
	if _, err := os.Stat(live.ServePath()); !os.IsNotExist(err) {
		t.Fatalf("advert remains: %v", err)
	}
	if _, err := live.Running(); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("running: %v", err)
	}
	if l := r.log.String(); !strings.Contains(l, "cull serve: started") || !strings.Contains(l, "cull serve: stopped") {
		t.Fatalf("log %q", l)
	}
}

func TestIdleExit(t *testing.T) {
	r := startRun(t, Options{Idle: 200 * time.Millisecond})
	wait(t, r)
	if _, err := LiveFor("cull").Running(); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("%v", err)
	}
}

func TestStopRequiresToken(t *testing.T) {
	r := startRun(t, Options{})
	live := LiveFor("cull")
	e, _ := live.Running()
	resp, err := http.Post(e.Base+"/api/stop", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatalf("stop without token: %d", resp.StatusCode)
	}
	time.Sleep(200 * time.Millisecond)
	if _, err := live.Running(); err != nil {
		t.Fatalf("server stopped: %v", err)
	}
	select {
	case err := <-r.done:
		t.Fatalf("run exited: %v", err)
	default:
	}
	_ = Stop(context.Background(), live)
	wait(t, r)
}

func TestIdleExitWithIdleStreamOpen(t *testing.T) {
	r := startRun(t, Options{Idle: 300 * time.Millisecond})
	e, _ := LiveFor("cull").Running()
	resp := get(t, e, "/api/events")
	defer func() { _ = resp.Body.Close() }()
	wait(t, r) // an open but idle tab doesn't keep the server alive
}

func TestAPIRequiresToken(t *testing.T) {
	r := startRun(t, Options{})
	e, _ := LiveFor("cull").Running()
	resp, err := http.Get(e.Base + "/api/poll")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatalf("no token: %d", resp.StatusCode)
	}
	for _, path := range []string{"/api/poll", "/api/ping"} {
		resp = get(t, e, path)
		_ = resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("%s with token: %d", path, resp.StatusCode)
		}
	}
	if resp, err = http.Get(e.Base + "/"); err != nil || resp.StatusCode != 200 {
		t.Fatalf("index: %v %v", resp, err)
	}
	_ = resp.Body.Close()
	_ = Stop(context.Background(), LiveFor("cull"))
	wait(t, r)
}
