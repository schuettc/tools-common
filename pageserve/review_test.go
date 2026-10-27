package pageserve

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// A claim never rewrites an entry a newer binary wrote: its fields this
// binary doesn't know would be lost and its version stamped back down.
func TestClaimLeavesANewerEntryAlone(t *testing.T) {
	l, _ := tempLive(t)
	raw := `{"v":99,"url":"http://127.0.0.1:9/","room":"content-future","page":"/tmp/x.md","pid":` + strconv.Itoa(os.Getpid()) + `,"owner":"","later":"kept"}`
	path := filepath.Join(l.Dir, "content-future.json")
	if err := os.MkdirAll(l.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	if ok, err := l.Claim("content-future", "sess-A"); err != nil || ok {
		t.Fatalf("claim of a newer entry: ok=%v err=%v, want false", ok, err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != raw {
		t.Fatalf("the newer entry was rewritten:\n%s", got)
	}
}

// A dropped report is logged without its token.
func TestADroppedReportLogsNoToken(t *testing.T) {
	var log bytes.Buffer
	ch := New(Spec{Tool: "cull"}, Identity{}, nil)
	ch.Log = &log
	for i := 0; i < cap(ch.reports)+1; i++ {
		ch.queueReport("landed", "secret-token-123")
	}
	if !strings.Contains(log.String(), "dropped report") {
		t.Fatalf("no drop logged: %q", log.String())
	}
	if strings.Contains(log.String(), "secret-token-123") {
		t.Fatalf("the token reached the log: %q", log.String())
	}
}

// The channel's stdout is the MCP stream: its help and flag errors go to
// stderr.
func TestChannelFlagOutputStaysOffStdout(t *testing.T) {
	c := cullCommands(t)
	cmd := c.ChannelCommand(strings.NewReader(""), Help{})
	for _, args := range [][]string{{"-h"}, {"--nope"}} {
		var out, errw bytes.Buffer
		_ = cmd.Run(args, &out, &errw)
		if out.Len() != 0 {
			t.Errorf("%v wrote to stdout: %q", args, out.String())
		}
	}
}

// A tool whose serve runs in the foreground (diazo: one server the channel
// starts) runs it in this process with no flag and opens no browser.
func TestAForegroundToolServesInThisProcess(t *testing.T) {
	c := cullCommands(t)
	c.Foreground = true
	var ran bool
	c.Serve = func(_ context.Context, port int, _ io.Writer) error { ran = port == 4321; return nil }
	opened := false
	old := openBrowser
	openBrowser = func(string) error { opened = true; return nil }
	t.Cleanup(func() { openBrowser = old })
	if err := c.ServeCommand(Help{}).Run([]string{"--port", "4321"}, io.Discard, io.Discard); err != nil || !ran || opened {
		t.Fatalf("err %v ran %v opened %v", err, ran, opened)
	}
}
