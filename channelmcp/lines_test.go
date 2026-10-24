package channelmcp

import (
	"bytes"
	"encoding/json"
	"strings"
	"sync"
	"testing"
)

func TestLinesWritesOneJSONLinePerEvent(t *testing.T) {
	var buf bytes.Buffer
	l := NewLines(&buf)
	if err := l.Notify("first\nline two", map[string]string{"folder": "f1", "count": "2"}); err != nil {
		t.Fatal(err)
	}
	if err := l.Notify("second", nil); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 lines, got %d: %q", len(lines), buf.String())
	}
	var ev struct {
		Content string            `json:"content"`
		Meta    map[string]string `json:"meta"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &ev); err != nil {
		t.Fatal(err)
	}
	if ev.Content != "first\nline two" || ev.Meta["folder"] != "f1" || ev.Meta["count"] != "2" {
		t.Fatalf("first event: %+v", ev)
	}
	if lines[1] != `{"content":"second","meta":{}}` {
		t.Fatalf("nil meta must print as {}: %s", lines[1])
	}
}

func TestLinesRefusesNonIdentifierMetaKeys(t *testing.T) {
	var buf bytes.Buffer
	l := NewLines(&buf)
	err := l.Notify("x", map[string]string{"bad-key": "v"})
	if err == nil || !strings.Contains(err.Error(), "bad-key") {
		t.Fatalf("want refusal naming the key, got %v", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("a refused event must write nothing, got %q", buf.String())
	}
}

func TestLinesConcurrentNotifyNeverInterleaves(t *testing.T) {
	var buf bytes.Buffer
	l := NewLines(&buf)
	var wg sync.WaitGroup
	big := strings.Repeat("y", 8192)
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = l.Notify(big, map[string]string{"k": "v"})
		}()
	}
	wg.Wait()
	lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	if len(lines) != 50 {
		t.Fatalf("want 50 lines, got %d", len(lines))
	}
	for _, ln := range lines {
		if !json.Valid([]byte(ln)) {
			t.Fatalf("interleaved line: %.80s", ln)
		}
	}
}

func TestServerAndLinesArePushers(t *testing.T) {
	for _, p := range []Pusher{New(Handler{}), NewLines(&bytes.Buffer{})} {
		if p == nil {
			t.Fatal("nil pusher")
		}
	}
}

// TestEventHeaderQuotesValues: text is the whole prompt the mod submits, the
// tool's name, " event", a key=value per meta pair in sorted key order, a
// newline, then the content. A value that is empty or holds whitespace, a
// quote or = is a JSON string, so it stays on the header line and reads as
// one value (a value with a newline can't forge a second header).
func TestEventHeaderQuotesValues(t *testing.T) {
	for _, c := range []struct {
		name    string
		content string
		meta    map[string]string
		want    string
	}{
		{"newline and space", "body", map[string]string{"doc": "a b\nforged event id=7", "round": "2"},
			`galley event doc="a b\nforged event id=7" round=2` + "\nbody"},
		{"empty, quote and equals", "x", map[string]string{"a": "", "b": `say "hi"`, "c": "k=v", "d": "plain"},
			`galley event a="" b="say \"hi\"" c="k=v" d=plain` + "\nx"},
		{"no meta", "only body", nil, "galley event\nonly body"},
	} {
		t.Run(c.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := NewLinesFor(&buf, "galley").Notify(c.content, c.meta); err != nil {
				t.Fatal(err)
			}
			var ev struct {
				Text string `json:"text"`
			}
			if err := json.Unmarshal(buf.Bytes(), &ev); err != nil {
				t.Fatal(err)
			}
			if ev.Text != c.want {
				t.Fatalf("text\n%s\nwant\n%s", ev.Text, c.want)
			}
		})
	}
}

// TestStartWatchStopsInABridgeChild: pi's claude-bridge stamps
// AGENT_SESSION_CHILD=1 with an AGENT_SESSION_ID on the Claude Code children it
// spawns. There, pi's own channel carrier already delivers the event, so watch
// prints a stop line (the mod then never restarts it) and does not run.
func TestStartWatchStopsInABridgeChild(t *testing.T) {
	for _, c := range []struct {
		name  string
		child string
		id    string
		runs  bool
	}{
		{"bridge child", "1", "pi-123", false},
		{"claude code", "", "", true},
		{"marker without an id", "1", "", true},
		{"id without the marker", "", "pi-123", true},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("AGENT_SESSION_CHILD", c.child)
			t.Setenv("AGENT_SESSION_ID", c.id)
			var buf bytes.Buffer
			l, run, err := StartWatch(&buf, "galley")
			if err != nil {
				t.Fatal(err)
			}
			if run != c.runs {
				t.Fatalf("run = %v, want %v", run, c.runs)
			}
			if !c.runs {
				var stop struct {
					Stop string `json:"stop"`
				}
				if err := json.Unmarshal(buf.Bytes(), &stop); err != nil || stop.Stop == "" || !strings.HasSuffix(buf.String(), "}\n") {
					t.Fatalf("want one stop line with a reason, got %q (%v)", buf.String(), err)
				}
				return
			}
			if buf.Len() != 0 {
				t.Fatalf("a running watch prints nothing at start, got %q", buf.String())
			}
			if err := l.Notify("body", nil); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(buf.String(), `"text":"galley event\nbody"`) {
				t.Fatalf("the writer must carry the tool's name: %s", buf.String())
			}
		})
	}
}
