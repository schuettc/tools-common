package pageserve

// What casebook's shell did that pageserve now does too (DCSN6): the loop
// never starts serve, a hook when the serve the loop reaches is new, the
// error code on StatusError, a quiet loop while no serve runs, the presence
// body as a hook, and in Run a stream override and an entry hook.

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/subaud-tools/tools-common/localweb"
)

// waitFor polls cond every 10ms for up to d.
func waitFor(d time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return cond()
}

// The wait loop never starts serve, so serve's idle exit can happen: only a
// tool call does.
func TestTheLoopNeverStartsServe(t *testing.T) {
	var starts atomic.Int32
	cl := NewClient(Live{Tool: "cull", Dir: t.TempDir()}, func() (Entry, error) {
		starts.Add(1)
		return Entry{}, errors.New("cannot start in a test")
	})
	ch := New(cullSpec(), Identity{Session: "s-1", Harness: "pi", CWD: t.TempDir()}, cl)
	ch.Retry, ch.Presence, ch.Poll = 20*time.Millisecond, 200*time.Millisecond, time.Second
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	_ = ch.Watch(ctx)
	if n := starts.Load(); n != 0 {
		t.Fatalf("the loop started serve %d times", n)
	}
	if _, err := ch.Client.Do(context.Background(), "GET", "/x", nil, nil); err == nil || starts.Load() != 1 {
		t.Fatalf("a tool call did not start serve: starts %d, %v", starts.Load(), err)
	}
}

// `<tool> report` posts through a client that never starts serve.
func TestReportNeverStartsServe(t *testing.T) {
	c := cullCommands(t)
	c.Process.Args = []string{"-test.run=^$"}
	var built atomic.Int32
	orig := newCommand
	newCommand = func(string, ...string) *exec.Cmd { built.Add(1); return exec.Command("false") }
	t.Cleanup(func() { newCommand = orig })
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	t.Setenv("AGENT_SESSION_ID", "s-1")
	t.Setenv("AGENT_SESSION_CHILD", "")
	err := c.ReportCommand().Run([]string{"turn-ended"}, io.Discard, io.Discard)
	if built.Load() != 0 || !errors.Is(err, ErrNoServe) {
		t.Fatalf("report started serve %d times: %v", built.Load(), err)
	}
}

// pi's reports from the channel never start serve either.
func TestChannelReportsNeverStartServe(t *testing.T) {
	var starts atomic.Int32
	cl := NewClient(Live{Tool: "cull", Dir: t.TempDir()}, func() (Entry, error) {
		starts.Add(1)
		return Entry{}, errors.New("cannot start in a test")
	})
	ch := New(cullSpec(), Identity{Session: "s-1", Harness: "pi", CWD: t.TempDir()}, cl)
	ch.Presence = time.Hour // the loop's presence is not what this counts
	c := connect(t, ch)
	c.notify("notifications/channels/report", map[string]string{"event": "turn_ended"})
	if waitFor(300*time.Millisecond, func() bool { return strings.Contains(c.logs.String(), "report turn_ended") }); starts.Load() != 0 {
		t.Fatalf("a report started serve %d times; log %q", starts.Load(), c.logs.String())
	}
}

// While no serve runs the loop logs nothing, poll after poll.
func TestTheLoopIsQuietWhileNoServeRuns(t *testing.T) {
	for _, tc := range []struct {
		name string
		spec func(Spec) Spec
	}{
		{"presence", func(s Spec) Spec { return s }},
		{"wait", func(s Spec) Spec { s.NoPresence = true; return s }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			log := &syncBuf{}
			ch := New(tc.spec(cullSpec()), Identity{Session: "s-1", Harness: "pi", CWD: t.TempDir()},
				NewClient(Live{Tool: "cull", Dir: t.TempDir()}, nil))
			ch.Log, ch.Retry, ch.Poll = log, 20*time.Millisecond, time.Second
			ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
			defer cancel()
			_ = ch.Watch(ctx)
			if log.String() != "" {
				t.Fatalf("log %q", log.String())
			}
		})
	}
}

// StatusError carries the JSON body's "code", so a caller can tell an
// unknown session from a missing message (both 404).
func TestStatusErrorCarriesTheCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := map[string]string{"error": "nope"}
		if r.URL.Path == "/session" {
			body["code"] = "unknown_session"
		}
		writeJSON(w, http.StatusNotFound, body)
	}))
	t.Cleanup(srv.Close)
	cl := NewClient(Live{Tool: "casebook"}, nil)
	cl.Find = func() (Entry, error) { return Entry{Base: srv.URL, Token: "t", PID: 1}, nil }
	for path, want := range map[string]string{"/session": "unknown_session", "/message": ""} {
		_, err := cl.Do(context.Background(), "GET", path, nil, nil)
		var se *StatusError
		if !errors.As(err, &se) || se.ErrCode != want || err.Error() != "casebook serve: 404 nope" {
			t.Errorf("%s: %#v %v, want code %q", path, se, err, want)
		}
	}
}

// Spec.PresenceBody replaces the presence body; Identity.PID is the
// harness's (the channel's parent).
func TestPresenceBodyHook(t *testing.T) {
	if got := FromEnv().PID; got != os.Getppid() {
		t.Fatalf("FromEnv PID %d, want the parent's %d", got, os.Getppid())
	}
	f := newFakeServe(t)
	spec := cullSpec()
	spec.PresenceBody = func(id Identity) any {
		return map[string]any{"id": id.Session, "cwd": id.CWD, "pid": id.PID}
	}
	id := Identity{Session: "s-1", Harness: "pi", CWD: "/w", PID: 42}
	connect(t, New(spec, id, f.client()))
	if !waitFor(5*time.Second, func() bool { return len(f.presences()) > 0 }) {
		t.Fatal("no presence")
	}
	want := map[string]any{"id": "s-1", "cwd": "/w", "pid": float64(42)}
	if got := f.presences()[0]; !reflect.DeepEqual(got, want) {
		t.Fatalf("presence %v, want %v", got, want)
	}
	// Announce posts the same body through the client it is given (a tool
	// call's, which may start serve).
	if err := New(spec, id, nil).Announce(context.Background(), f.client()); err != nil {
		t.Fatal(err)
	}
	if got := f.presences(); !reflect.DeepEqual(got[len(got)-1], want) {
		t.Fatalf("announce %v, want %v", got[len(got)-1], want)
	}
}

// Spec.OnServe runs when the serve the loop reaches is a new process (its
// entry's StartedAt changed), not on the first serve it finds; and the loop
// registers presence with the new serve at once.
func TestOnServeRunsWhenServeIsNew(t *testing.T) {
	f := newFakeServe(t)
	var mu sync.Mutex
	started := time.Date(2026, 10, 27, 1, 0, 0, 0, time.UTC)
	cl := f.client()
	cl.Find = func() (Entry, error) {
		mu.Lock()
		defer mu.Unlock()
		return Entry{Base: f.srv.URL, Token: "tok", PID: os.Getpid(), StartedAt: started}, nil
	}
	seen := make(chan Entry, 4)
	spec := cullSpec()
	spec.OnServe = func(_ context.Context, ch *Channel, e Entry) {
		if ch.ID.Session != "s-1" {
			t.Errorf("OnServe channel %+v", ch.ID)
		}
		seen <- e
	}
	ch := New(spec, Identity{Session: "s-1", Harness: "pi", CWD: t.TempDir()}, cl)
	ch.Retry, ch.Presence, ch.Poll = 20*time.Millisecond, time.Hour, time.Second
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = ch.Watch(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	if !waitFor(5*time.Second, func() bool { return f.waits.Load() > 0 }) {
		t.Fatal("never waited")
	}
	select {
	case e := <-seen:
		t.Fatalf("OnServe ran for the first serve: %+v", e)
	case <-time.After(300 * time.Millisecond):
	}
	before := len(f.presences())
	mu.Lock()
	started = started.Add(time.Hour)
	mu.Unlock()
	select {
	case e := <-seen:
		if !e.StartedAt.Equal(started) {
			t.Fatalf("OnServe entry %+v", e)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("OnServe never ran for the new serve")
	}
	if !waitFor(5*time.Second, func() bool { return len(f.presences()) > before }) {
		t.Fatal("no presence for the new serve")
	}
}

// Options.Stream serves GET /api/events instead of the Events log, so a tool
// with its own event bus keeps its stream.
func TestRunStreamOverride(t *testing.T) {
	startRun(t, Options{Stream: func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "the tool's stream") }})
	e, _ := LiveFor("cull").Running()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", e.Base+"/api/events", nil)
	req.Header.Set(localweb.TokenHeader, e.Token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if string(b) != "the tool's stream" {
		t.Fatalf("/api/events = %q", b)
	}
}

// Options.Entry edits the entry before it is written: the server's own start
// time, and Reopened.
func TestRunEntryHook(t *testing.T) {
	at := time.Date(2026, 10, 27, 1, 2, 3, 0, time.UTC)
	startRun(t, Options{Entry: func(e *Entry) { e.StartedAt, e.Reopened = at, true }})
	e, err := LiveFor("cull").Running()
	if err != nil || !e.StartedAt.Equal(at) || !e.Reopened {
		t.Fatalf("%+v %v", e, err)
	}
	b, _ := os.ReadFile(LiveFor("cull").Dir + "/serve.json")
	var raw map[string]any
	_ = json.Unmarshal(b, &raw)
	if raw["reopened"] != true {
		t.Fatalf("serve.json %s", b)
	}
}
