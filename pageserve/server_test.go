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
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	tools "github.com/subaud-tools/tools-common"
	"github.com/subaud-tools/tools-common/localweb"
	"github.com/subaud-tools/tools-common/localweb/page"
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
	o.Live, o.Assets = LiveFor("cull"), testPage
	if o.API == nil {
		o.API = testAPI()
	}
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

// A page served by a tool holds the cookie: it must not be able to stop its
// server. The Origin is the page's own, so localweb's cross-origin guard lets
// the request through and only the stop route can refuse it.
func TestStopRefusesTheCookieAlone(t *testing.T) {
	r := startRun(t, Options{})
	live := LiveFor("cull")
	e, _ := live.Running()
	resp, err := (&http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Get(e.URL)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	cookies := resp.Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies %v", cookies)
	}
	for _, header := range []string{"", "not-the-token"} {
		req, _ := http.NewRequest(http.MethodPost, e.Base+"/api/stop", strings.NewReader("{}"))
		req.AddCookie(cookies[0])
		req.Header.Set("Origin", e.Base)
		if header != "" {
			req.Header.Set(localweb.TokenHeader, header)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("stop with the cookie and header %q: %d", header, resp.StatusCode)
		}
	}
	time.Sleep(200 * time.Millisecond)
	select {
	case err := <-r.done:
		t.Fatalf("the cookie stopped the server: %v", err)
	default:
	}
	if err := Stop(context.Background(), live); err != nil {
		t.Fatal(err)
	}
	wait(t, r)
}

// sendToAPI makes a call to the running serve's API in the background, so a test
// can hold its handler open.
func sendToAPI(t *testing.T, live Live) {
	t.Helper()
	e, err := live.Running()
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodPost, e.Base+"/api/write", strings.NewReader("{}"))
	req.Header.Set(localweb.TokenHeader, e.Token)
	go func() {
		if resp, err := http.DefaultClient.Do(req); err == nil {
			_ = resp.Body.Close()
		}
	}()
}

// A handler still writing holds the entry and OnStop back, so a restart can't
// start beside it.
func TestStopWaitsForAHandlerStillWriting(t *testing.T) {
	t.Setenv("CULL_HOME", t.TempDir())
	live := LiveFor("cull")
	entered, finish := make(chan struct{}), make(chan struct{})
	var returned atomic.Bool
	var returnedAtOnStop bool
	r := runHere(t, Options{
		API: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			close(entered)
			<-finish
			returned.Store(true)
		}),
		OnStop: func(context.Context) { returnedAtOnStop = returned.Load() },
	})
	sendToAPI(t, live)
	<-entered
	stopped := make(chan error, 1)
	go func() { stopped <- Stop(context.Background(), live) }()
	time.Sleep(500 * time.Millisecond)
	_, err := live.Running()
	close(finish)
	if err != nil {
		t.Errorf("the entry went while a handler was writing: %v", err)
	}
	if err := <-stopped; err != nil {
		t.Fatal(err)
	}
	wait(t, r)
	if !returnedAtOnStop {
		t.Error("OnStop ran while a handler was writing")
	}
}

// A handler that never returns and an OnStop that ignores its context cost the
// wait and a line in the log, and still leave inside the time Stop waits.
func TestStopIsBoundedWhenShutdownWillNotEnd(t *testing.T) {
	t.Setenv("CULL_HOME", t.TempDir())
	live := LiveFor("cull")
	entered, finish := make(chan struct{}), make(chan struct{})
	defer close(finish)
	r := runHere(t, Options{
		API: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			close(entered)
			<-finish
		}),
		OnStop: func(context.Context) { <-finish },
	})
	sendToAPI(t, live)
	<-entered
	if err := Stop(context.Background(), live); err != nil {
		t.Fatal(err)
	}
	wait(t, r)
	log := r.log.String()
	for _, want := range []string{"requests still running", "OnStop overran"} {
		if !strings.Contains(log, want) {
			t.Errorf("log has no %q: %s", want, log)
		}
	}
}

// A restart can't overlap the old serve's shutdown: while OnStop runs the server
// takes no requests and its entry is still there, so a second serve won't start,
// and once the entry is gone it does.
func TestShutdownFinishesBeforeTheEntryGoes(t *testing.T) {
	t.Setenv("CULL_HOME", t.TempDir())
	live := LiveFor("cull")
	stopping, finish := make(chan error, 1), make(chan struct{})
	var deadline bool
	r := runHere(t, Options{OnStop: func(ctx context.Context) {
		_, deadline = ctx.Deadline()
		_, err := live.Running()
		stopping <- err
		<-finish
	}})
	e, _ := live.Running()
	req, _ := http.NewRequest(http.MethodPost, e.Base+"/api/stop", strings.NewReader("{}"))
	req.Header.Set(localweb.TokenHeader, e.Token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	select {
	case err := <-stopping:
		if err != nil {
			t.Fatalf("the entry went before OnStop: %v", err)
		}
	case <-time.After(5 * time.Second):
		close(finish)
		t.Fatal("OnStop never ran")
	}
	if resp, err := http.Get(e.Base + "/api/poll"); err == nil {
		_ = resp.Body.Close()
		t.Error("the server still takes requests during OnStop")
	}
	if err := Run(context.Background(), Options{Live: live}); err == nil || !strings.Contains(err.Error(), "already running") {
		t.Errorf("a second serve during OnStop: %v", err)
	}
	close(finish)
	wait(t, r)
	if !deadline {
		t.Error("OnStop's context has no deadline")
	}
	if _, err := live.Running(); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("the entry stayed: %v", err)
	}
	second := runHere(t, Options{})
	if _, err := live.Running(); err != nil {
		t.Fatalf("no second serve: %v", err)
	}
	_ = Stop(context.Background(), live)
	wait(t, second)
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

// Every answer of the API says which build is serving: the tool's routes, the
// shared ones and a route that isn't there.
func TestEveryAPIResponseCarriesTheBuild(t *testing.T) {
	r := startRun(t, Options{Version: "1.2.3"})
	e, _ := LiveFor("cull").Running()
	want := buildOf("1.2.3", testPage)
	if !regexp.MustCompile(`^1\.2\.3\+[0-9a-f]{12}$`).MatchString(want) {
		t.Fatalf("build %q", want)
	}
	for _, path := range []string{"/api/ping", "/api/poll", "/api/nothing"} {
		resp := get(t, e, path)
		_ = resp.Body.Close()
		if got := resp.Header.Get(BuildHeader); got != want {
			t.Fatalf("%s: %s %q, want %q", path, BuildHeader, got, want)
		}
	}
	_ = Stop(context.Background(), LiveFor("cull"))
	wait(t, r)
}

// The build changes with the version and with any asset, wherever it is, the
// kit's included: a rebuild with one version still counts as a change.
func TestTheBuildHashesEveryAsset(t *testing.T) {
	tree := func(body string) fstest.MapFS {
		return fstest.MapFS{"index.html": {Data: []byte("page")}, "deep/in/app.js": {Data: []byte(body)}}
	}
	base := buildOf("1", tree("a"))
	if got := buildOf("1", tree("a")); got != base {
		t.Fatalf("not stable: %q, %q", base, got)
	}
	if got := buildOf("2", tree("a")); got == base {
		t.Fatal("the version doesn't count")
	}
	if got := buildOf("1", tree("b")); got == base {
		t.Fatal("a nested asset doesn't count")
	}
	moved := fstest.MapFS{"index.html": {Data: []byte("page")}, "deep/app.js": {Data: []byte("a")}}
	if got := buildOf("1", moved); got == base {
		t.Fatal("a file's name doesn't count")
	}
	joined := fstest.MapFS{"a": {Data: []byte("bc")}}
	if buildOf("1", joined) == buildOf("1", fstest.MapFS{"a": {Data: []byte("b")}, "c": {Data: nil}}) {
		t.Fatal("a file's bounds don't count")
	}
	if withKit := buildOf("1", page.With(tree("a"))); withKit == base {
		t.Fatal("the kit's files aren't walked")
	}
	if !strings.HasPrefix(buildOf("1", nil), "1+") {
		t.Fatal("no assets")
	}
}
