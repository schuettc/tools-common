package pageserve

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeBinary is a file standing in for serve's binary, and how Run finds it.
func fakeBinary(t *testing.T) string {
	t.Helper()
	exe := filepath.Join(t.TempDir(), "cull")
	if err := os.WriteFile(exe, []byte("old build"), 0o700); err != nil {
		t.Fatal(err)
	}
	return exe
}

// install replaces exe the way an install does: a new file renamed over it.
func install(t *testing.T, exe string) {
	t.Helper()
	if err := os.WriteFile(exe+".new", []byte("the new build"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(exe+".new", exe); err != nil {
		t.Fatal(err)
	}
}

func staleEvents(l *Events) int {
	evs, _, _ := l.after(0)
	n := 0
	for _, e := range evs {
		if e.Type == "stale" {
			n++
		}
	}
	return n
}

func TestAReplacedBinaryMarksTheEntryAndEmitsOnce(t *testing.T) {
	exe := fakeBinary(t)
	was, every := executable, staleEvery
	executable, staleEvery = func() string { return exe }, 5*time.Millisecond
	t.Cleanup(func() { executable, staleEvery = was, every })
	at := time.Date(2026, 10, 27, 1, 2, 3, 0, time.UTC)
	evs := NewEvents()
	startRun(t, Options{Events: evs, Entry: func(e *Entry) { e.StartedAt = at }})
	live := LiveFor("cull")
	time.Sleep(30 * time.Millisecond)
	if e, err := live.Running(); err != nil || e.Stale {
		t.Fatalf("stale before any install: %+v %v", e, err)
	}
	install(t, exe)
	if !waitFor(5*time.Second, func() bool { e, err := live.Running(); return err == nil && e.Stale }) {
		t.Fatal("the entry was never marked stale")
	}
	if !waitFor(5*time.Second, func() bool { return staleEvents(evs) == 1 }) {
		t.Fatalf("%d stale events", staleEvents(evs))
	}
	time.Sleep(30 * time.Millisecond)
	if n := staleEvents(evs); n != 1 {
		t.Fatalf("%d stale events, want one", n)
	}
	if e, _ := live.Running(); !e.StartedAt.Equal(at) || e.PID != os.Getpid() {
		t.Fatalf("marking lost the entry's fields: %+v", e)
	}
}

// A tick can run before the entry is written. The install it saw is not used
// up: stale is recorded only once the entry says so.
func TestATickBeforeTheEntryIsWrittenIsNotLost(t *testing.T) {
	exe := fakeBinary(t)
	live, evs := Live{Tool: "cull", Dir: t.TempDir()}, NewEvents()
	w, err := watchBinary(exe, live, evs)
	if err != nil {
		t.Fatal(err)
	}
	install(t, exe)
	w.check()
	if n := staleEvents(evs); n != 0 {
		t.Fatalf("%d stale events before the entry exists", n)
	}
	if err := live.Advertise(Entry{PID: os.Getpid(), Token: "x"}); err != nil {
		t.Fatal(err)
	}
	w.check()
	if e, err := live.Running(); err != nil || !e.Stale {
		t.Fatalf("the install was lost: %+v %v", e, err)
	}
	if n := staleEvents(evs); n != 1 {
		t.Fatalf("%d stale events", n)
	}
}

// Only this serve's own entry is marked: another's is left alone.
func TestAnotherServesEntryIsNotMarked(t *testing.T) {
	exe := fakeBinary(t)
	live, evs := Live{Tool: "cull", Dir: t.TempDir()}, NewEvents()
	w, err := watchBinary(exe, live, evs)
	if err != nil {
		t.Fatal(err)
	}
	if err := live.Advertise(Entry{PID: os.Getppid(), Token: "x"}); err != nil {
		t.Fatal(err)
	}
	install(t, exe)
	w.check()
	if e, err := live.Running(); err != nil || e.Stale || staleEvents(evs) != 0 {
		t.Fatalf("%+v %v, %d events", e, err, staleEvents(evs))
	}
}

// Mid-install the file may briefly be missing: that is no install yet.
func TestAMissingBinaryIsNotAnInstall(t *testing.T) {
	exe := fakeBinary(t)
	live, evs := Live{Tool: "cull", Dir: t.TempDir()}, NewEvents()
	w, err := watchBinary(exe, live, evs)
	if err != nil {
		t.Fatal(err)
	}
	if err := live.Advertise(Entry{PID: os.Getpid(), Token: "x"}); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(exe); err != nil {
		t.Fatal(err)
	}
	w.check()
	if e, _ := live.Running(); e.Stale {
		t.Fatal("marked stale with no new binary")
	}
}

// A serve that can't find its binary still serves, and says so.
func TestABinaryThatCannotBeWatchedIsLogged(t *testing.T) {
	was := executable
	executable = func() string { return "" }
	t.Cleanup(func() { executable = was })
	r := startRun(t, Options{})
	if l := r.log.String(); !strings.Contains(l, "not watching") {
		t.Fatalf("log %q", l)
	}
}

// The next tool call replaces a stale serve; the agent never asks for it.
func TestAToolCallRestartsAStaleServe(t *testing.T) {
	running := Entry{PID: 1, Stale: true}
	var stopped, started int
	cl := NewClient(Live{Tool: "cull", Dir: t.TempDir()}, func() (Entry, error) {
		started++
		running = Entry{PID: 2}
		return running, nil
	})
	cl.Find = func() (Entry, error) {
		if stopped > started {
			return Entry{}, ErrNotRunning
		}
		return running, nil
	}
	cl.Stop = func(context.Context) error { stopped++; return nil }
	spec := cullSpec()
	spec.NoPresence = true
	var sawPID int
	spec.Call = func(context.Context, *Channel, string, json.RawMessage) (string, error) {
		sawPID = running.PID
		return "ok", nil
	}
	c := connect(t, New(spec, Identity{CWD: t.TempDir()}, cl))
	m := c.call("tools/call", map[string]any{"name": "cull_where", "arguments": map[string]any{}})
	if m.Error != nil {
		t.Fatal(string(m.Error))
	}
	if stopped != 1 || started != 1 || sawPID != 2 {
		t.Fatalf("stopped %d, started %d, the tool ran against pid %d", stopped, started, sawPID)
	}
}

// A tool call says why when the stale serve can't be replaced.
func TestAToolCallReportsAFailedRestart(t *testing.T) {
	cl := NewClient(Live{Tool: "cull", Dir: t.TempDir()}, func() (Entry, error) { return Entry{PID: 2}, nil })
	cl.Find = func() (Entry, error) { return Entry{PID: 1, Stale: true}, nil }
	cl.Stop = func(context.Context) error { return errors.New("did not stop") }
	spec := cullSpec()
	spec.Call = func(context.Context, *Channel, string, json.RawMessage) (string, error) {
		return "", errors.New("the tool ran")
	}
	c := connect(t, New(spec, Identity{CWD: t.TempDir()}, cl))
	m := c.call("tools/call", map[string]any{"name": "cull_where", "arguments": map[string]any{}})
	if got := fmt.Sprint(string(m.Result), string(m.Error)); !strings.Contains(got, "did not stop") {
		t.Fatalf("%s", got)
	}
}

// Two channels race to replace one stale serve: the one that finds it gone
// has nothing to stop, and starts or finds the new one.
func TestFreshAcceptsAServeAnotherChannelStopped(t *testing.T) {
	var started int
	c := NewClient(Live{Tool: "cull"}, func() (Entry, error) { started++; return Entry{PID: 2}, nil })
	finds := 0
	c.Find = func() (Entry, error) {
		finds++
		if finds == 1 {
			return Entry{PID: 1, Stale: true}, nil
		}
		return Entry{}, ErrNotRunning
	}
	c.Stop = func(context.Context) error { return fmt.Errorf("cull %w", ErrNotRunning) }
	if err := c.Fresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if started != 1 {
		t.Fatalf("started %d", started)
	}
}

// Channels finding one stale serve at once replace it once: the second must
// not stop the serve the first just started.
func TestFreshRacingChannelsStopOnce(t *testing.T) {
	t.Setenv("CULL_HOME", t.TempDir())
	var mu sync.Mutex
	stale, stops, starts := true, 0, 0
	find := func() (Entry, error) {
		mu.Lock()
		defer mu.Unlock()
		return Entry{Stale: stale}, nil
	}
	newClient := func() *Client {
		c := NewClient(LiveFor("cull"), func() (Entry, error) {
			mu.Lock()
			defer mu.Unlock()
			starts++
			return Entry{}, nil
		})
		c.Find = find
		c.Stop = func(context.Context) error {
			time.Sleep(50 * time.Millisecond) // the others find it stale meanwhile
			mu.Lock()
			defer mu.Unlock()
			stops++
			stale = false
			return nil
		}
		return c
	}
	var wg sync.WaitGroup
	for range 8 {
		c := newClient()
		wg.Go(func() {
			if err := c.Fresh(context.Background()); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if stops != 1 {
		t.Fatalf("stopped %d times, want 1", stops)
	}
}
