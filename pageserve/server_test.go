package pageserve

// Moved from cull's internal/cull/serve/serve_test.go: Run with a stand-in
// page and API where cull served its review over its store.

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	tools "github.com/subaud-tools/tools-common"
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
	return runHere(t, o)
}

// runHere is startRun in the state directory already set, so a test can
// restart a serve over the same state.
func runHere(t *testing.T, o Options) *running {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	// Serve writes its state as it stops, so the test's folder outlives it.
	t.Cleanup(func() { cancel(); <-stopped })
	r := &running{done: make(chan error, 1), url: make(chan string, 1), log: &strings.Builder{}, started: make(chan string, 1)}
	o.Live, o.Assets, o.API = LiveFor("cull"), testPage, testAPI()
	o.Log = &lockedWriter{w: r.log}
	o.OnStart = func(_ context.Context, u string) { r.started <- u }
	o.Ready = func(u string) { r.url <- u }
	go func() { r.done <- Run(ctx, o); close(stopped) }()
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

// The list reaches the tool before the server can answer, a poll that names its
// tab checks it in there, and it outlives a restart: the next page open reuses
// the tab, which only checks in once a minute when hidden.
func TestTabsAreTheToolsAndSurviveARestart(t *testing.T) {
	t.Setenv("CULL_HOME", t.TempDir())
	live := LiveFor("cull")
	var tabs *localweb.Tabs
	var atStart *localweb.Tabs
	o := Options{Tabs: func(l *localweb.Tabs) { tabs = l }}
	r := runHere(t, o)
	atStart = tabs
	if atStart == nil {
		t.Fatal("Options.Tabs was not called")
	}
	e, err := live.Running()
	if err != nil {
		t.Fatal(err)
	}
	if c := tabs.Pick("plan"); c.Kind != localweb.TabNone {
		t.Fatalf("before a check-in: %+v", c)
	}
	_ = get(t, e, "/api/poll?tab=t1&page=plan").Body.Close()
	if c := tabs.Pick("plan"); c.Kind != localweb.TabHere || c.Tab != "t1" {
		t.Fatalf("after a check-in: %+v", c)
	}
	if err := Stop(context.Background(), live); err != nil {
		t.Fatal(err)
	}
	wait(t, r)

	r = runHere(t, o)
	if tabs == atStart {
		t.Fatal("the restart kept the old list")
	}
	if c := tabs.Pick("plan"); c.Kind != localweb.TabHere || c.Tab != "t1" {
		t.Fatalf("after a restart: %+v", c)
	}
	if err := Stop(context.Background(), live); err != nil {
		t.Fatal(err)
	}
	wait(t, r)
}

// With no callback the tabs are still kept: a tool gets the rule for nothing.
func TestTabsAreKeptWithNoCodeInTheTool(t *testing.T) {
	r := startRun(t, Options{})
	e, _ := LiveFor("cull").Running()
	_ = get(t, e, "/api/poll?tab=t1&page=plan").Body.Close()
	b, err := os.ReadFile(filepath.Join(tools.StateDir("cull"), "tabs.json"))
	if err != nil || !strings.Contains(string(b), `"t1"`) {
		t.Fatalf("%s %v", b, err)
	}
	_ = Stop(context.Background(), LiveFor("cull"))
	wait(t, r)
}

// A list the tool already put on its Events is the one kept, so a tool with its
// own clock in a test, or not yet moved off its own list, isn't handed a second.
func TestTabsAlreadyOnTheEventsAreKept(t *testing.T) {
	own := localweb.NewTabs(nil)
	evs := NewEvents()
	evs.Tabs = own
	var got *localweb.Tabs
	r := startRun(t, Options{Events: evs, Tabs: func(l *localweb.Tabs) { got = l }})
	if got != own {
		t.Fatal("Run replaced the list on the Events")
	}
	_ = Stop(context.Background(), LiveFor("cull"))
	wait(t, r)
	if _, err := os.Stat(filepath.Join(tools.StateDir("cull"), "tabs.json")); err != nil {
		t.Fatalf("not kept: %v", err)
	}
}
