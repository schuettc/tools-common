package pageserve

// Moved from cull's internal/cull/serve/api_test.go: its tests of the event
// log, with an emit of a fixed event where cull stored an answer.

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/subaud-tools/tools-common/localweb"
)

type eventsFixture struct {
	t  *testing.T
	ev *Events
	h  http.Handler
}

func newEventsFixture(t *testing.T) *eventsFixture {
	ev := NewEvents()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/events", ev.Stream)
	mux.HandleFunc("GET /api/poll", ev.Poll)
	return &eventsFixture{t: t, ev: ev, h: mux}
}

func (f *eventsFixture) ServeHTTP(w http.ResponseWriter, r *http.Request) { f.h.ServeHTTP(w, r) }

// emit is one event, as cull's answer store emits on every change.
func (f *eventsFixture) emit() { f.ev.Emit("answers", map[string]int64{"project": 1}) }

func (f *eventsFixture) do(method, path string) *httptest.ResponseRecorder {
	f.t.Helper()
	w := httptest.NewRecorder()
	f.h.ServeHTTP(w, httptest.NewRequest(method, path, nil))
	return w
}

type pollResp struct {
	Cursor string      `json:"cursor"`
	Reset  bool        `json:"reset"`
	Events []wireEvent `json:"events"`
}

func (f *eventsFixture) poll(since string) pollResp {
	f.t.Helper()
	w := f.do("GET", "/api/poll?since="+since)
	var r pollResp
	if err := json.Unmarshal(w.Body.Bytes(), &r); err != nil {
		f.t.Fatal(err)
	}
	return r
}

// cur is this log's cursor for event n.
func (f *eventsFixture) cur(n int) string { return fmt.Sprintf("%s-%d", f.ev.boot, n) }

func TestCursorIsTheLatest(t *testing.T) {
	f := newEventsFixture(t)
	f.emit()
	if got := f.ev.Cursor(); got != f.cur(1) {
		t.Fatalf("cursor %q", got)
	}
}

func TestPollCursorSemantics(t *testing.T) {
	f := newEventsFixture(t)
	f.emit()
	f.emit()
	r := f.poll("")
	if r.Cursor != f.cur(2) || len(r.Events) != 2 || r.Reset {
		t.Fatalf("%+v", r)
	}
	if r = f.poll(f.cur(1)); len(r.Events) != 1 || r.Reset || r.Cursor != f.cur(2) {
		t.Fatalf("since 1: %+v", r)
	}
	if r = f.poll(f.cur(2)); len(r.Events) != 0 || r.Reset {
		t.Fatalf("since 2: %+v", r)
	}
	if r = f.poll(f.cur(0)); len(r.Events) != 2 || r.Reset {
		t.Fatalf("since 0: %+v", r)
	}
}

func TestPollResetCases(t *testing.T) {
	f := newEventsFixture(t)
	f.emit()
	for name, since := range map[string]string{
		"other boot": "deadbeef-1",
		"malformed":  "banana",
		"old format": "1",
		"ahead":      f.cur(99),
		"negative":   f.cur(-1),
	} {
		r := f.poll(since)
		if !r.Reset || len(r.Events) != 0 || r.Cursor != f.cur(1) {
			t.Fatalf("%s: %+v", name, r)
		}
	}
	w := f.do("GET", "/api/poll?since="+f.cur(1))
	if strings.Contains(w.Body.String(), `"reset":true`) {
		t.Fatalf("valid since reset: %s", w.Body)
	}
	if !strings.Contains(w.Body.String(), `"events":[]`) {
		t.Fatalf("events must be an array: %s", w.Body)
	}
}

func TestRetentionCap(t *testing.T) {
	f := newEventsFixture(t)
	for i := 0; i < 1005; i++ {
		f.ev.Emit("answers", map[string]int{"i": i})
	}
	r := f.poll("")
	if len(r.Events) != keepEvents || r.Events[0].Type != "answers" || !strings.Contains(string(r.Events[0].Data), `"i":5`) {
		t.Fatalf("retained %d, first %s", len(r.Events), r.Events[0].Data)
	}
	if r = f.poll(f.cur(3)); !r.Reset || len(r.Events) != 0 {
		t.Fatalf("evicted since: %+v", r)
	}
	if r = f.poll(f.cur(5)); r.Reset || len(r.Events) != keepEvents {
		t.Fatalf("oldest-valid since: reset=%v n=%d", r.Reset, len(r.Events))
	}
}

// sse opens /api/events and returns a channel of "id|data" strings.
func sse(t *testing.T, url, lastID string) (<-chan string, func()) {
	t.Helper()
	return sseReq(t, url+"/api/events", lastID)
}

// sseSince connects the way the kit's live() does: the cursor in ?since=.
func sseSince(t *testing.T, url, since string) (<-chan string, func()) {
	t.Helper()
	return sseReq(t, url+"/api/events?since="+since, "")
}

func sseReq(t *testing.T, u, lastID string) (<-chan string, func()) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	req, _ := http.NewRequestWithContext(ctx, "GET", u, nil)
	if lastID != "" {
		req.Header.Set("Last-Event-ID", lastID)
	}
	resp, err := http.DefaultClient.Do(req) //nolint:bodyclose // the reader goroutine below closes it
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	out := make(chan string, 100)
	go func() {
		defer close(out)
		defer func() { _ = resp.Body.Close() }()
		sc := bufio.NewScanner(resp.Body)
		var id string
		for sc.Scan() {
			l := sc.Text()
			if v, ok := strings.CutPrefix(l, "id: "); ok {
				id = v
			}
			if v, ok := strings.CutPrefix(l, "data: "); ok {
				out <- id + "|" + v
			}
		}
	}()
	return out, cancel
}

func next(t *testing.T, c <-chan string) string {
	t.Helper()
	select {
	case v, ok := <-c:
		if !ok {
			t.Fatal("stream closed")
		}
		return v
	case <-time.After(3 * time.Second):
		t.Fatal("no event")
	}
	return ""
}

func TestSSEResumeSendsExactlyMissed(t *testing.T) {
	f := newEventsFixture(t)
	for i := 0; i < 4; i++ {
		f.emit()
	}
	ts := httptest.NewServer(f)
	defer ts.Close()
	c, cancel := sse(t, ts.URL, f.cur(2))
	defer cancel()
	for _, n := range []int{3, 4} {
		if v := next(t, c); !strings.HasPrefix(v, f.cur(n)+"|") || strings.Contains(v, "reset") {
			t.Fatalf("event %d: %s", n, v)
		}
	}
	f.emit()
	if v := next(t, c); !strings.HasPrefix(v, f.cur(5)+"|") {
		t.Fatalf("live: %s", v)
	}
}

func TestSSEResetCases(t *testing.T) {
	f := newEventsFixture(t)
	for i := 0; i < 1005; i++ {
		f.ev.Emit("answers", map[string]int{"i": i})
	}
	ts := httptest.NewServer(f)
	defer ts.Close()
	for name, last := range map[string]string{
		"other boot": "deadbeef-3",
		"evicted":    f.cur(2),
		"malformed":  "banana",
		"ahead":      f.cur(5000),
	} {
		c, cancel := sse(t, ts.URL, last)
		want := f.cur(int(f.ev.latest())) + `|{"type":"reset","data":{}}`
		if v := next(t, c); v != want {
			t.Fatalf("%s: got %q want %q", name, v, want)
		}
		f.emit() // live events follow the reset
		if v := next(t, c); !strings.Contains(v, `"type":"answers"`) || strings.Contains(v, "reset") {
			t.Fatalf("%s live: %s", name, v)
		}
		cancel()
	}
}

func TestSSEFreshConnectNoReset(t *testing.T) {
	f := newEventsFixture(t)
	f.emit()
	ts := httptest.NewServer(f)
	defer ts.Close()
	c, cancel := sse(t, ts.URL, "")
	defer cancel()
	f.emit()
	if v := next(t, c); !strings.HasPrefix(v, f.cur(2)+"|") {
		t.Fatalf("fresh: %s", v)
	}
}

func TestSSEDisconnectEndsStream(t *testing.T) {
	f := newEventsFixture(t)
	ts := httptest.NewServer(f)
	defer ts.Close()
	c, cancel := sse(t, ts.URL, "")
	f.emit()
	next(t, c)
	if n := f.ev.Streams(); n != 1 {
		t.Fatalf("streams %d", n)
	}
	cancel()
	deadline := time.Now().Add(3 * time.Second)
	for f.ev.Streams() != 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if n := f.ev.Streams(); n != 0 {
		t.Fatalf("stream goroutine still running: %d", n)
	}
}

// stalledWriter accepts the first write, then blocks every later write until
// its write deadline passes, like a client that stopped reading.
type stalledWriter struct {
	h         http.Header
	mu        sync.Mutex
	deadline  time.Time
	writes    int
	deadlines int
}

func (w *stalledWriter) Header() http.Header { return w.h }
func (w *stalledWriter) WriteHeader(int)     {}
func (w *stalledWriter) Flush()              {}
func (w *stalledWriter) SetWriteDeadline(t time.Time) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.deadline = t
	w.deadlines++
	return nil
}
func (w *stalledWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	w.writes++
	n, dl := w.writes, w.deadline
	w.mu.Unlock()
	if n == 1 {
		return len(p), nil
	}
	if dl.IsZero() {
		select {} // no deadline set: the stream would hang forever
	}
	time.Sleep(time.Until(dl))
	return 0, os.ErrDeadlineExceeded
}

func TestSSEWriteDeadlineEndsStalledStream(t *testing.T) {
	f := newEventsFixture(t)
	f.ev.WriteTimeout = 50 * time.Millisecond
	w := &stalledWriter{h: http.Header{}}
	done := make(chan struct{})
	go func() {
		f.ev.Stream(w, httptest.NewRequest("GET", "/api/events", nil))
		close(done)
	}()
	time.Sleep(50 * time.Millisecond)
	f.emit()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("stalled stream did not end")
	}
	if w.deadlines == 0 {
		t.Fatal("no write deadline set")
	}
}

func TestSSEFlushesAndDelivers(t *testing.T) {
	f := newEventsFixture(t)
	ts := httptest.NewServer(f)
	defer ts.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", ts.URL+"/api/events", nil)
	resp, err := http.DefaultClient.Do(req) // returns only once headers are flushed
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content-type %q", ct)
	}
	f.emit()
	sc := bufio.NewScanner(resp.Body)
	var id, data string
	for sc.Scan() {
		l := sc.Text()
		if v, ok := strings.CutPrefix(l, "id: "); ok {
			id = v
		}
		if v, ok := strings.CutPrefix(l, "data: "); ok {
			data = v
			break
		}
	}
	if id != f.cur(1) || !strings.Contains(data, `"type":"answers"`) {
		t.Fatalf("id %q data %q", id, data)
	}
}

// The kit's live() resumes through ?since=, and ?since= wins over a header.
func TestSSEResumeFromSinceQuery(t *testing.T) {
	f := newEventsFixture(t)
	for i := 0; i < 4; i++ {
		f.emit()
	}
	ts := httptest.NewServer(f)
	defer ts.Close()
	c, cancel := sseSince(t, ts.URL, f.cur(2))
	defer cancel()
	for _, n := range []int{3, 4} {
		if v := next(t, c); !strings.HasPrefix(v, f.cur(n)+"|") || strings.Contains(v, "reset") {
			t.Fatalf("event %d: %s", n, v)
		}
	}
	c2, cancel2 := sseSince(t, ts.URL, "banana")
	defer cancel2()
	if v := next(t, c2); !strings.Contains(v, `"type":"reset"`) {
		t.Fatalf("malformed since: %s", v)
	}
}

// A poll or a stream that names its tab tells Tabs (diazo's check-ins): the
// tab then shows its page.
func TestTabsHearFromPollsAndStreams(t *testing.T) {
	f := newEventsFixture(t)
	f.ev.Tabs = localweb.NewTabs(nil)
	f.do("GET", "/api/poll?tab=a&page=p1")
	if c := f.ev.Tabs.Pick("p1"); c.Kind != localweb.TabHere || c.Tab != "a" {
		t.Fatalf("after a poll: %+v", c)
	}
	ts := httptest.NewServer(f)
	defer ts.Close()
	c, cancel := sseReq(t, ts.URL+"/api/events?tab=b&page=p2", "")
	defer cancel()
	f.emit()
	next(t, c)
	if got := f.ev.Tabs.Pick("p2"); got.Kind != localweb.TabHere || got.Tab != "b" {
		t.Fatalf("with a stream open: %+v", got)
	}
}
