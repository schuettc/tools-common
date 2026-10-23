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
