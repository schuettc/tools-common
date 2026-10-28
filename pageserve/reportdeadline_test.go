package pageserve

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

// A report to a serve that takes the connection but never answers gives up
// at ReportTimeout: the Claude Code plugin's hook runs it and waits on it.
func TestAReportToAHungServeGivesUpAtItsDeadline(t *testing.T) {
	c := cullCommands(t)
	release := make(chan struct{})
	hung := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	t.Cleanup(hung.Close)
	t.Cleanup(func() { close(release) }) // runs first: the handler lets go before Close waits on it
	if err := c.Process.Live.Advertise(Entry{URL: hung.URL, Base: hung.URL, Token: "x", PID: os.Getpid(), Version: "1"}); err != nil {
		t.Fatal(err)
	}
	old := ReportTimeout
	ReportTimeout = 300 * time.Millisecond
	t.Cleanup(func() { ReportTimeout = old })
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	t.Setenv("AGENT_SESSION_ID", "s-1")
	t.Setenv("AGENT_SESSION_CHILD", "")
	start := time.Now()
	err := c.ReportCommand().Run([]string{"turn-ended"}, io.Discard, io.Discard)
	if took := time.Since(start); took > 3*time.Second {
		t.Fatalf("the report held its caller %v (err %v); want about %v", took, err, ReportTimeout)
	}
	if err == nil {
		t.Fatal("a report to a hung serve succeeded")
	}
}

// Commands.Open, when set, opens the page instead of the browser opener, so a
// tool's own switch (casebook's CASEBOOK_NO_BROWSER) covers `<tool> serve`.
func TestServeOpensThroughTheToolsOpener(t *testing.T) {
	c := cullCommands(t)
	var got string
	c.Open = func(url string) error { got = url; return nil }
	sys := false
	old := openBrowser
	openBrowser = func(string) error { sys = true; return nil }
	t.Cleanup(func() { openBrowser = old })
	if err := c.Process.Live.Advertise(Entry{URL: "http://127.0.0.1:1/?t=x", Base: "http://127.0.0.1:1", Token: "x", PID: os.Getpid(), Version: "1"}); err != nil {
		t.Fatal(err)
	}
	if err := c.ServeCommand(Help{}).Run(nil, io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	if got != "http://127.0.0.1:1/?t=x" || sys {
		t.Fatalf("opened %q through the tool's opener; system opener used: %v", got, sys)
	}
}
