package pageserve

// The event log behind GET /api/events (SSE) and GET /api/poll, in the shape
// the page kit's live.js reads. Moved from cull's internal/cull/serve/events.go,
// with diazo's tab check-ins.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/subaud-tools/tools-common/localweb"
)

const keepEvents = 1000

const defaultWriteTimeout = 10 * time.Second

type event struct {
	Cursor int64           `json:"-"`
	Type   string          `json:"type"`
	Data   json.RawMessage `json:"data"`
}

// Events is an in-memory log of the tool's events with increasing integer
// cursors, and the poll and stream handlers the page reads it with. A cursor
// on the wire is "<boot>-<n>", so a page that outlives a serve restart is told
// to reload instead of missing what happened in between.
type Events struct {
	// WriteTimeout bounds each frame a stream writes (0: 10s), so a client
	// that stopped reading ends its stream instead of pinning it.
	WriteTimeout time.Duration
	// Tabs hears from the page's tabs: a poll that names its tab is a
	// check-in, and a tab counts as open while its stream is. Run sets it when
	// nil (see Options.Tabs).
	Tabs *localweb.Tabs

	boot   string
	mu     sync.Mutex
	evs    []event
	next   int64
	notify chan struct{} // closed and replaced on every emit

	life    context.Context // Run's context; streams end with it
	wg      sync.WaitGroup
	streams atomic.Int32 // open event streams
}

// NewEvents returns an empty log with a new boot id.
func NewEvents() *Events {
	l := &Events{notify: make(chan struct{})}
	var b [4]byte
	_, _ = rand.Read(b[:])
	l.boot = hex.EncodeToString(b[:])
	return l
}

// format renders the wire cursor for event n: "<boot>-<n>".
func (l *Events) format(n int64) string { return l.boot + "-" + strconv.FormatInt(n, 10) }

// Cursor is the wire cursor of the latest event. A handler that answers with
// a snapshot reads it before the snapshot's data, so an event racing the reads
// is redelivered to the client, never skipped.
func (l *Events) Cursor() string { return l.format(l.latest()) }

// Emit appends an event of type typ with data as its JSON payload.
func (l *Events) Emit(typ string, data any) {
	b, err := json.Marshal(data)
	if err != nil {
		return
	}
	l.mu.Lock()
	l.next++
	l.evs = append(l.evs, event{Cursor: l.next, Type: typ, Data: b})
	if len(l.evs) > keepEvents {
		l.evs = append([]event(nil), l.evs[len(l.evs)-keepEvents:]...)
	}
	close(l.notify)
	l.notify = make(chan struct{})
	l.mu.Unlock()
}

// after returns the events with cursor > since, the latest cursor, and the
// channel that closes on the next emit.
func (l *Events) after(since int64) ([]event, int64, <-chan struct{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []event
	for _, e := range l.evs {
		if e.Cursor > since {
			out = append(out, e)
		}
	}
	return out, l.next, l.notify
}

func (l *Events) latest() int64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.next
}

// resume parses a client cursor. ok is false (the client must reload) when
// it is malformed, from another boot, ahead of the log, or older than the
// oldest retained event.
func (l *Events) resume(v string) (int64, bool) {
	boot, num, found := strings.Cut(v, "-")
	if !found || boot != l.boot {
		return 0, false
	}
	n, err := strconv.ParseInt(num, 10, 64)
	if err != nil || n < 0 {
		return 0, false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	oldest := l.next + 1 // first event the client would need next
	if len(l.evs) > 0 {
		oldest = l.evs[0].Cursor
	}
	if n > l.next || n < oldest-1 {
		return 0, false
	}
	return n, true
}

// Streams is how many event streams are open.
func (l *Events) Streams() int { return int(l.streams.Load()) }

// bind ends every stream when ctx ends (Run's context).
func (l *Events) bind(ctx context.Context) {
	l.mu.Lock()
	l.life = ctx
	l.mu.Unlock()
}

// wait returns once every stream has ended.
func (l *Events) wait() { l.wg.Wait() }

type wireEvent struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// Poll is GET /api/poll?since=<cursor>: the events after the cursor, or
// reset when the client must reload.
func (l *Events) Poll(w http.ResponseWriter, r *http.Request) {
	// A poll that names its tab is a check-in (a hidden tab's, once a minute).
	if tab := r.URL.Query().Get("tab"); tab != "" && l.Tabs != nil {
		l.Tabs.Seen(tab, r.URL.Query().Get("page"))
	}
	v := r.URL.Query().Get("since")
	since, ok := int64(0), true // no cursor: everything retained
	if v != "" {
		since, ok = l.resume(v)
	}
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{"cursor": l.format(l.latest()), "reset": true, "events": []wireEvent{}})
		return
	}
	evs, cur, _ := l.after(since)
	out := make([]wireEvent, 0, len(evs))
	for _, e := range evs {
		out = append(out, wireEvent{e.Type, e.Data})
	}
	writeJSON(w, http.StatusOK, map[string]any{"cursor": l.format(cur), "events": out})
}

// Stream is GET /api/events: the events as server-sent events, from the
// client's cursor on, until the client goes or the server stops.
func (l *Events) Stream(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	l.wg.Add(1)
	defer l.wg.Done()
	l.streams.Add(1)
	defer l.streams.Add(-1)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	l.mu.Lock()
	life := l.life
	l.mu.Unlock()
	if life != nil {
		defer context.AfterFunc(life, cancel)()
	}
	// The tab counts as open while its stream is.
	if tab := r.URL.Query().Get("tab"); tab != "" && l.Tabs != nil {
		defer l.Tabs.Connected(tab, r.URL.Query().Get("page"))()
	}
	rc := http.NewResponseController(w)
	timeout := l.WriteTimeout
	if timeout <= 0 {
		timeout = defaultWriteTimeout
	}
	// write sends one frame under a fresh write deadline, so a client that
	// stopped reading ends its stream instead of pinning it.
	write := func(format string, args ...any) bool {
		_ = rc.SetWriteDeadline(time.Now().Add(timeout))
		if _, err := fmt.Fprintf(w, format, args...); err != nil {
			return false
		}
		fl.Flush()
		return true
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	// The kit's live() sends its cursor as ?since= (an EventSource cannot set
	// Last-Event-ID on its first connection); a browser's own reconnect sends
	// the header. ?since= wins.
	var cursor int64
	lastID := r.URL.Query().Get("since")
	if lastID == "" {
		lastID = r.Header.Get("Last-Event-ID")
	}
	reset := false
	if lastID == "" {
		cursor = l.latest()
	} else if cursor, ok = l.resume(lastID); !ok {
		cursor, reset = l.latest(), true
		if !write(": ok\n\nid: %s\ndata: {\"type\":\"reset\",\"data\":{}}\n\n", l.format(cursor)) {
			return
		}
	}
	if !reset && !write(": ok\n\n") {
		return
	}
	beat := time.NewTicker(15 * time.Second)
	defer beat.Stop()
	for {
		evs, _, wait := l.after(cursor)
		for _, e := range evs {
			b, _ := json.Marshal(wireEvent{e.Type, e.Data})
			if !write("id: %s\ndata: %s\n\n", l.format(e.Cursor), b) {
				return
			}
			cursor = e.Cursor
		}
		select {
		case <-ctx.Done():
			return
		case <-wait:
		case <-beat.C:
			if !write(": hb\n\n") {
				return
			}
		}
	}
}
