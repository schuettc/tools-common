package pageserve

// Moved from cull's internal/cull/channel tests (channel_test.go, report_test.go,
// watch_test.go) and diazo's fresh_test.go. cull's ran its channel against
// its real serve; the agent routes stay each tool's, so these run against
// fakeServe, which answers presence, wait and report the way cull's serve does.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/subaud-tools/tools-common/channelmcp"
)

// ---- a fake serve -----------------------------------------------------------

type fakeSend struct {
	id      int64
	root    string
	text    string
	claimed bool
}

// fakeServe is a serve's agent routes: presence, a wait that hands each Send
// to exactly one session waiting from its root, and report.
type fakeServe struct {
	mu       sync.Mutex
	sends    []*fakeSend
	presence []map[string]any
	waits    atomic.Int32
	reports  chan map[string]string
	srv      *httptest.Server
}

func newFakeServe(t *testing.T) *fakeServe {
	f := &fakeServe{reports: make(chan map[string]string, 8)}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/agent/presence", func(w http.ResponseWriter, r *http.Request) {
		var b map[string]any
		_ = json.NewDecoder(r.Body).Decode(&b)
		f.mu.Lock()
		f.presence = append(f.presence, b)
		f.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /api/agent/wait", func(w http.ResponseWriter, r *http.Request) {
		f.waits.Add(1)
		root := r.URL.Query().Get("root")
		deadline := time.Now().Add(200 * time.Millisecond)
		for time.Now().Before(deadline) {
			f.mu.Lock()
			for _, s := range f.sends {
				if !s.claimed && s.root == root {
					s.claimed = true
					f.mu.Unlock()
					writeJSON(w, http.StatusOK, map[string]any{"id": s.id, "root": s.root, "text": s.text, "report": "0123456789abcdef"})
					return
				}
			}
			f.mu.Unlock()
			select {
			case <-r.Context().Done():
				return
			case <-time.After(10 * time.Millisecond):
			}
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /api/agent/report", func(w http.ResponseWriter, r *http.Request) {
		var b map[string]string
		_ = json.NewDecoder(r.Body).Decode(&b)
		f.reports <- b
		w.WriteHeader(http.StatusNoContent)
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeServe) send(root, text string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sends = append(f.sends, &fakeSend{id: int64(len(f.sends) + 1), root: root, text: text})
}

func (f *fakeServe) presences() []map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]map[string]any(nil), f.presence...)
}

func (f *fakeServe) client() *Client {
	cl := NewClient(Live{Tool: "cull"}, nil)
	cl.Find = func() (Entry, error) { return Entry{Base: f.srv.URL, Token: "tok", PID: os.Getpid()}, nil }
	return cl
}

// ---- cull's channel, as a Spec ----------------------------------------------

var errNoProject = errors.New("not in a project")

// cullSpec is cull's channel: one tool that answers with the session's root,
// a project root (any folder but "/nowhere"), and cull's wake.
func cullSpec() Spec {
	return Spec{
		Tool: "cull", Version: "test", Instructions: "cull's rules", Answers: "Court's answers",
		Tools: []channelmcp.Tool{{Name: "cull_where", Description: "the project root", InputSchema: json.RawMessage(`{"type":"object"}`)}},
		Call: func(_ context.Context, ch *Channel, name string, _ json.RawMessage) (string, error) {
			if name != "cull_where" {
				return "", errors.New("unknown tool")
			}
			return ch.Root()
		},
		Root: func(cwd string) (string, error) {
			if cwd == "/nowhere" {
				return "", errNoProject
			}
			return cwd, nil
		},
		Wake: func(body json.RawMessage) (string, map[string]string, bool) {
			var got struct {
				ID     int64  `json:"id"`
				Root   string `json:"root"`
				Text   string `json:"text"`
				Report string `json:"report"`
			}
			if json.Unmarshal(body, &got) != nil || got.ID == 0 {
				return "", nil, false
			}
			return got.Text, map[string]string{"source": "cull", "project": got.Root, "send_id": strconv.FormatInt(got.ID, 10),
				channelmcp.MetaReport: got.Report}, true
		},
	}
}

// ---- an in-memory MCP client ----------------------------------------------

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) { s.mu.Lock(); defer s.mu.Unlock(); return s.b.Write(p) }
func (s *syncBuf) String() string              { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }

type rpc struct {
	ID     *int            `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  json.RawMessage `json:"error"`
}

type pushed struct {
	Content string            `json:"content"`
	Meta    map[string]string `json:"meta"`
}

type mcpClient struct {
	t      *testing.T
	w      io.Writer
	mu     sync.Mutex
	next   int
	wait   map[int]chan rpc
	events chan pushed
	logs   *syncBuf
}

func connect(t *testing.T, ch *Channel) *mcpClient {
	t.Helper()
	logs := &syncBuf{}
	ch.Log = logs
	ch.Retry, ch.Presence, ch.Poll = 50*time.Millisecond, 200*time.Millisecond, time.Second
	c := &mcpClient{t: t, wait: map[int]chan rpc{}, events: make(chan pushed, 16), logs: logs}
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	c.w = inW
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = ch.Run(ctx, inR, outW); close(done) }()
	go func() {
		sc := bufio.NewScanner(outR)
		sc.Buffer(make([]byte, 1<<20), 1<<24)
		for sc.Scan() {
			var m rpc
			if json.Unmarshal(sc.Bytes(), &m) != nil {
				t.Errorf("not an MCP frame on stdout: %q", sc.Text())
				continue
			}
			if m.ID == nil {
				if m.Method == "notifications/claude/channel" {
					var ev pushed
					_ = json.Unmarshal(m.Params, &ev)
					c.events <- ev
				}
				continue
			}
			c.mu.Lock()
			w := c.wait[*m.ID]
			c.mu.Unlock()
			if w != nil {
				w <- m
			}
		}
	}()
	t.Cleanup(func() {
		cancel()
		_ = inW.Close()
		<-done
		_ = outW.Close()
	})
	c.call("initialize", map[string]any{"protocolVersion": "2025-06-18"})
	return c
}

func (c *mcpClient) call(method string, params any) rpc {
	c.t.Helper()
	c.mu.Lock()
	c.next++
	id := c.next
	ch := make(chan rpc, 1)
	c.wait[id] = ch
	c.mu.Unlock()
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	if _, err := c.w.Write(append(b, '\n')); err != nil {
		c.t.Fatal(err)
	}
	select {
	case m := <-ch:
		return m
	case <-time.After(10 * time.Second):
		c.t.Fatalf("no answer to %s", method)
	}
	return rpc{}
}

// notify sends the channel a notification, as channels.tools does.
func (c *mcpClient) notify(method string, params any) {
	c.t.Helper()
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
	if _, err := c.w.Write(append(b, '\n')); err != nil {
		c.t.Fatal(err)
	}
}

func (c *mcpClient) expectEvent(d time.Duration) (pushed, bool) {
	select {
	case ev := <-c.events:
		return ev, true
	case <-time.After(d):
		return pushed{}, false
	}
}

// ---- tests -----------------------------------------------------------------

func TestInitializeToolsAndCall(t *testing.T) {
	f := newFakeServe(t)
	root := t.TempDir()
	c := connect(t, New(cullSpec(), Identity{Session: "s-1", Harness: "pi", Label: "pi · a", CWD: root}, f.client()))
	init := c.call("initialize", map[string]any{"protocolVersion": "2025-06-18"})
	var ir struct {
		ServerInfo   struct{ Name, Version string } `json:"serverInfo"`
		Instructions string                         `json:"instructions"`
	}
	if err := json.Unmarshal(init.Result, &ir); err != nil || ir.ServerInfo.Name != "cull" || ir.ServerInfo.Version != "test" || ir.Instructions != "cull's rules" {
		t.Fatalf("initialize: %s %v", init.Result, err)
	}
	m := c.call("tools/call", map[string]any{"name": "cull_where", "arguments": map[string]any{}})
	var r struct {
		Content []struct{ Text string } `json:"content"`
		IsError bool                    `json:"isError"`
	}
	if err := json.Unmarshal(m.Result, &r); err != nil || r.IsError || len(r.Content) != 1 || r.Content[0].Text != root {
		t.Fatalf("cull_where: %s %v", m.Result, err)
	}
}

func TestSendReachesThisChannelOnceAndNoOtherProject(t *testing.T) {
	f := newFakeServe(t)
	rootA, rootB := t.TempDir(), t.TempDir()
	a := connect(t, New(cullSpec(), Identity{Session: "s-1", Harness: "pi", Label: "pi · a", CWD: rootA}, f.client()))
	b := connect(t, New(cullSpec(), Identity{Session: "s-2", Harness: "pi", Label: "pi · b", CWD: rootB}, f.client()))
	f.send(rootA, "Court sent his answers for "+filepath.Base(rootA))
	ev, ok := a.expectEvent(5 * time.Second)
	if !ok {
		t.Fatal("channel A never got the send")
	}
	if !strings.Contains(ev.Content, "Court sent his answers for "+filepath.Base(rootA)) {
		t.Errorf("content %q", ev.Content)
	}
	if ev.Meta["source"] != "cull" || ev.Meta["project"] != rootA || ev.Meta["send_id"] != "1" || ev.Meta[channelmcp.MetaReport] != "0123456789abcdef" {
		t.Errorf("meta = %v", ev.Meta)
	}
	if _, again := a.expectEvent(500 * time.Millisecond); again {
		t.Error("the send was delivered twice")
	}
	if ev, got := b.expectEvent(300 * time.Millisecond); got {
		t.Errorf("a channel in another project got the send: %s", ev.Content)
	}
}

// A delivery the tool's Wake refuses is logged by its size and field names,
// never its text, so a lost delivery leaves a trace (diazo's overdue wake
// vanished silently) and Court's words stay out of the log.
func TestARefusedDeliveryIsLoggedWithoutItsText(t *testing.T) {
	f := newFakeServe(t)
	root := t.TempDir()
	spec := cullSpec()
	spec.Wake = func(json.RawMessage) (string, map[string]string, bool) { return "", nil, false }
	c := connect(t, New(spec, Identity{Session: "s-1", Harness: "pi", Label: "pi · a", CWD: root}, f.client()))
	f.send(root, "private words of Court's")
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(c.logs.String(), "refused delivery:") && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	logs := c.logs.String()
	i := strings.Index(logs, "cull channel: refused delivery: ")
	if i < 0 {
		t.Fatalf("logs = %q", logs)
	}
	line, _, _ := strings.Cut(logs[i:], "\n")
	if !strings.Contains(line, " bytes") || !strings.Contains(line, "id") {
		t.Errorf("line = %q, want its size and field names", line)
	}
	if strings.Contains(line, "private words") {
		t.Errorf("the delivery's text was logged: %q", line)
	}
	if ev, got := c.expectEvent(300 * time.Millisecond); got {
		t.Errorf("a refused delivery was pushed: %s", ev.Content)
	}
}

// Presence names the session and the folder its Sends come from; a tool
// without a project names no root, and one whose serve has no presence
// route (diazo's) only waits.
func TestPresence(t *testing.T) {
	root := t.TempDir()
	id := Identity{Session: "s-1", Harness: "pi", Label: "pi · a", CWD: root}
	for _, tc := range []struct {
		name string
		spec func(Spec) Spec
		want []map[string]any
	}{
		{"project", func(s Spec) Spec { return s }, []map[string]any{{"session": "s-1", "harness": "pi", "label": "pi · a", "root": root}}},
		{"no project", func(s Spec) Spec { s.Root = nil; return s }, []map[string]any{{"session": "s-1", "harness": "pi", "label": "pi · a"}}},
		{"no presence", func(s Spec) Spec { s.NoPresence = true; return s }, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeServe(t)
			connect(t, New(tc.spec(cullSpec()), id, f.client()))
			deadline := time.Now().Add(5 * time.Second)
			for f.waits.Load() == 0 && time.Now().Before(deadline) {
				time.Sleep(10 * time.Millisecond)
			}
			if f.waits.Load() == 0 {
				t.Fatal("the channel never waited")
			}
			got := f.presences()
			if tc.want == nil && len(got) != 0 || tc.want != nil && (len(got) == 0 || !reflect.DeepEqual(got[0], tc.want[0])) {
				t.Fatalf("presence %v, want %v", got, tc.want)
			}
		})
	}
}

// The loop runs only with a session and, for a tool with projects, a project.
func TestWatchSaysWhyItCannotRun(t *testing.T) {
	f := newFakeServe(t)
	for _, tc := range []struct {
		id   Identity
		want string
	}{
		{Identity{CWD: t.TempDir()}, "no session id (not under Claude Code or pi); Court's answers will not reach this session"},
		{Identity{Session: "s-1", CWD: "/nowhere"}, errNoProject.Error()},
	} {
		err := New(cullSpec(), tc.id, f.client()).Watch(context.Background())
		if err == nil || err.Error() != tc.want {
			t.Errorf("%+v: %v, want %q", tc.id, err, tc.want)
		}
	}
}

// pi's reports reach serve in the order sent, and a slow serve never holds
// up the channel's answers to the agent's tool calls.
func TestReportsReachServeInOrderWithoutBlockingToolCalls(t *testing.T) {
	release := make(chan struct{})
	posted := make(chan map[string]string, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/agent/report":
			var b map[string]string
			_ = json.NewDecoder(r.Body).Decode(&b)
			<-release
			posted <- b
		case "/api/agent/wait":
			select {
			case <-r.Context().Done():
			case <-time.After(100 * time.Millisecond):
			}
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	cl := NewClient(Live{Tool: "cull"}, nil)
	cl.Find = func() (Entry, error) { return Entry{Base: srv.URL, Token: "tok", PID: os.Getpid()}, nil }
	c := connect(t, New(cullSpec(), Identity{Session: "s1", Harness: "pi", Label: "pi · s1", CWD: t.TempDir()}, cl))
	c.notify("notifications/channels/report", map[string]string{"event": "landed", "report": "0123456789abcdef"})
	c.notify("notifications/channels/report", map[string]string{"event": "turn_ended"})
	done := make(chan struct{})
	go func() {
		c.call("tools/list", map[string]any{})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("a tools call waited on serve's report answer")
	}
	close(release)
	want := []map[string]string{
		{"session": "s1", "event": "landed", "token": "0123456789abcdef"},
		{"session": "s1", "event": "turn_ended", "token": ""},
	}
	for i, w := range want {
		select {
		case got := <-posted:
			if !reflect.DeepEqual(got, w) {
				t.Fatalf("report %d posted %v, want %v", i+1, got, w)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("report %d never reached serve", i+1)
		}
	}
}

// `<tool> watch` is the channel's presence and wait loop with a JSON line on
// stdout for each Send, which is what the Claude Code plugin's mod reads.
func TestWatchPrintsASendAsOneLine(t *testing.T) {
	f := newFakeServe(t)
	root := t.TempDir()
	id := Identity{Session: "s-1", Harness: "claude", Label: "claude · one", CWD: root}
	r, w := io.Pipe()
	watch := New(cullSpec(), id, f.client())
	watch.Retry, watch.Presence, watch.Poll = 50*time.Millisecond, 200*time.Millisecond, time.Second
	watch.Push = channelmcp.NewLinesFor(w, "cull")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = watch.Watch(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	f.send(root, "Court sent his answers")
	lines := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(r)
		if sc.Scan() {
			lines <- sc.Text()
		}
	}()
	var line string
	select {
	case line = <-lines:
	case <-time.After(5 * time.Second):
		t.Fatal("no line printed for the Send")
	}
	var ev pushed
	if err := json.Unmarshal([]byte(line), &ev); err != nil {
		t.Fatalf("not one JSON line: %q (%v)", line, err)
	}
	if ev.Meta["source"] != "cull" || ev.Meta["project"] != root || ev.Meta["send_id"] == "" || !strings.Contains(ev.Content, "Court sent his answers") {
		t.Fatalf("line = %+v", ev)
	}
}

// Under Claude Code the plugin's mod runs `<tool> watch`, so `<tool> channel`
// must not take the Sends too: it serves the tools and never waits. Under pi
// it waits and pushes.
func TestChannelWaitsOnlyOutsideClaudeCode(t *testing.T) {
	for _, tc := range []struct {
		name       string
		claude, pi string
		waits      bool
	}{
		{"pi", "", "s-1", true},
		{"claude code", "s-1", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeServe(t)
			root, _ := filepath.EvalSymlinks(t.TempDir())
			t.Setenv("CLAUDE_CODE_SESSION_ID", tc.claude)
			t.Setenv("AGENT_SESSION_ID", tc.pi)
			t.Setenv("AGENT_SESSION_CHILD", "")
			t.Chdir(root)
			ch := ForSession(cullSpec(), f.client())
			c := connect(t, ch)
			f.send(ch.scope(), "Court sent his answers")
			if tc.waits {
				if _, ok := c.expectEvent(5 * time.Second); !ok {
					t.Fatal("the channel under pi never got the send")
				}
				return
			}
			if ev, ok := c.expectEvent(700 * time.Millisecond); ok {
				t.Fatalf("the channel took a send under Claude Code; only watch may: %s", ev.Content)
			}
			if !strings.Contains(c.logs.String(), "the cull plugin's `cull watch` delivers Court's answers") {
				t.Fatalf("log %q", c.logs.String())
			}
		})
	}
}

func TestClientStartsServeWhenNotRunning(t *testing.T) {
	l := Live{Tool: "cull", Dir: t.TempDir()}
	started := 0
	cl := NewClient(l, func() (Entry, error) { started++; return Entry{}, errors.New("cannot start in a test") })
	_, err := cl.Do(context.Background(), "GET", "/api/agent/status", nil, nil)
	if started != 1 || err == nil || !strings.Contains(err.Error(), "starting cull serve: cannot start") {
		t.Errorf("started %d, err %v", started, err)
	}
	// A client that doesn't start serve says it isn't running.
	_, err = NewClient(l, nil).Do(context.Background(), "GET", "/x", nil, nil)
	if !errors.Is(err, ErrNoServe) || err.Error() != "cull serve isn't running" {
		t.Errorf("err = %v", err)
	}
}

func TestClientSerializesServeStarts(t *testing.T) {
	var mu sync.Mutex
	var live *Entry
	var starts, active, maxActive int32
	cl := NewClient(Live{Tool: "cull"}, func() (Entry, error) {
		n := atomic.AddInt32(&active, 1)
		if n > atomic.LoadInt32(&maxActive) {
			atomic.StoreInt32(&maxActive, n)
		}
		atomic.AddInt32(&starts, 1)
		time.Sleep(100 * time.Millisecond)
		atomic.AddInt32(&active, -1)
		a := Entry{Base: "http://127.0.0.1:1", Token: "t", PID: 1}
		mu.Lock()
		live = &a
		mu.Unlock()
		return a, nil
	})
	cl.Find = func() (Entry, error) {
		mu.Lock()
		defer mu.Unlock()
		if live == nil {
			return Entry{}, ErrNotRunning
		}
		return *live, nil
	}
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, _ = cl.Do(context.Background(), "GET", "/x", nil, nil) }()
	}
	wg.Wait()
	if starts != 1 || maxActive != 1 {
		t.Errorf("starts %d, max concurrent %d; want 1, 1", starts, maxActive)
	}
}

// A non-2xx answer names the tool's serve.
func TestStatusErrorNamesTheTool(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "not yours"})
	}))
	t.Cleanup(srv.Close)
	cl := NewClient(Live{Tool: "cull"}, nil)
	cl.Find = func() (Entry, error) { return Entry{Base: srv.URL, Token: "t", PID: 1}, nil }
	code, err := cl.Do(context.Background(), "POST", "/api/agent/reply", map[string]int{"id": 1}, nil)
	var se *StatusError
	if code != http.StatusConflict || !errors.As(err, &se) || err.Error() != "cull serve: 409 not yours" {
		t.Fatalf("%d %v", code, err)
	}
}

func TestFreshRestartsOnlyAStaleServe(t *testing.T) {
	running := Entry{PID: 1, Stale: true}
	var stopped, started int
	c := NewClient(Live{Tool: "diazo"}, func() (Entry, error) {
		started++
		running = Entry{PID: 2}
		return running, nil
	})
	c.Find = func() (Entry, error) {
		if stopped > started {
			return Entry{}, ErrNotRunning
		}
		return running, nil
	}
	c.Stop = func(context.Context) error { stopped++; return nil }

	if err := c.Fresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if stopped != 1 || started != 1 || running.PID != 2 {
		t.Fatalf("stale serve: stopped %d, started %d, running pid %d", stopped, started, running.PID)
	}
	if err := c.Fresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if stopped != 1 || started != 1 {
		t.Fatalf("fresh serve was restarted: stopped %d, started %d", stopped, started)
	}
}

func TestQueryLeavesOutEmptyValues(t *testing.T) {
	if got := Query("session", "s 1", "root", "", "timeout", "55"); got != "session=s+1&timeout=55" {
		t.Fatalf("%q", got)
	}
}
