package channelmcp

import (
	"bytes"
	"encoding/json"
	"log"
	"os"
	"strings"
	"testing"
)

type gotReport struct {
	Event ReportEvent
	Token string
}

// pingAfter sends a ping and waits for its answer: Run handles lines in
// order, so every report sent before it has been handled by then.
func pingAfter(t *testing.T, h Handler, lines ...string) {
	t.Helper()
	w, sc := drive(t, h)
	for _, l := range lines {
		send(t, w, l)
	}
	send(t, w, `{"jsonrpc":"2.0","id":99,"method":"ping"}`)
	if m := read(t, sc); m["id"] != float64(99) {
		t.Fatalf("a report was answered, or the ping was not: %v", m)
	}
}

func TestReportCallsOnReportForEachEvent(t *testing.T) {
	var got []gotReport
	h := Handler{Name: "m", Version: "1", OnReport: func(e ReportEvent, token string) {
		got = append(got, gotReport{e, token})
	}}
	pingAfter(t, h,
		`{"jsonrpc":"2.0","method":"notifications/channels/report","params":{"event":"held","report":"t7"}}`,
		`{"jsonrpc":"2.0","method":"notifications/channels/report","params":{"event":"landed","report":"t7"}}`,
		`{"jsonrpc":"2.0","method":"notifications/channels/report","params":{"event":"turn_ended","report":""}}`,
		`{"jsonrpc":"2.0","method":"notifications/channels/report","params":{"event":"turn_ended"}}`,
	)
	want := []gotReport{{ReportHeld, "t7"}, {ReportLanded, "t7"}, {ReportTurnEnded, ""}, {ReportTurnEnded, ""}}
	if len(got) != len(want) {
		t.Fatalf("OnReport calls = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("call %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestMalformedReportIsIgnoredAndLogged(t *testing.T) {
	var logs bytes.Buffer
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	var got []gotReport
	h := Handler{Name: "m", Version: "1", OnReport: func(e ReportEvent, token string) {
		got = append(got, gotReport{e, token})
	}}
	pingAfter(t, h,
		`{"jsonrpc":"2.0","method":"notifications/channels/report","params":{"event":"delivered","report":"t7"}}`,
		`{"jsonrpc":"2.0","method":"notifications/channels/report","params":{"event":"landed"}}`,
		`{"jsonrpc":"2.0","method":"notifications/channels/report","params":"landed t7"}`,
	)
	if len(got) != 0 {
		t.Errorf("a malformed report reached OnReport: %+v", got)
	}
	if n := strings.Count(logs.String(), "channelmcp: ignored report"); n != 3 {
		t.Errorf("want 3 logged ignores, got %d:\n%s", n, logs.String())
	}
}

func TestReportWithNoHandlerIsIgnored(t *testing.T) {
	pingAfter(t, Handler{Name: "m", Version: "1"},
		`{"jsonrpc":"2.0","method":"notifications/channels/report","params":{"event":"landed","report":"t7"}}`)
}

func TestLinesCarryTheReportToken(t *testing.T) {
	var buf bytes.Buffer
	l := NewLinesFor(&buf, "diazo")
	if err := l.Notify("hi", map[string]string{MetaReport: "t7", "folder": "f"}); err != nil {
		t.Fatal(err)
	}
	if err := l.Notify("no token", map[string]string{"folder": "f"}); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	var first, second map[string]any
	_ = json.Unmarshal([]byte(lines[0]), &first)
	_ = json.Unmarshal([]byte(lines[1]), &second)
	if first["report"] != "t7" {
		t.Errorf("watch line lacks the report token: %s", lines[0])
	}
	if _, ok := second["report"]; ok {
		t.Errorf("an event with no token must print no report field: %s", lines[1])
	}
}
