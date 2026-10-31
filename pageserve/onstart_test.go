package pageserve

import (
	"context"
	"errors"
	"testing"
	"time"
)

// OnStart runs before the entry is written, so a channel that finds the
// entry finds the tool's own start-up done (cull sets its page URL there).
func TestOnStartRunsBeforeTheEntryIsWritten(t *testing.T) {
	t.Setenv("CULL_HOME", t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	// Serve writes its state as it stops, so the test's folder outlives it.
	t.Cleanup(func() { cancel(); <-done })
	live := LiveFor("cull")
	seen := make(chan error, 1)
	ready := make(chan struct{}, 1)
	o := Options{Live: live, Assets: testPage, API: testAPI(),
		OnStart: func(context.Context, string) { _, err := live.Running(); seen <- err },
		Ready:   func(string) { ready <- struct{}{} }}
	go func() { done <- Run(ctx, o) }()
	select {
	case <-ready:
	case err := <-done:
		t.Fatalf("run exited: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("not ready")
	}
	if err := <-seen; !errors.Is(err, ErrNotRunning) {
		t.Fatalf("during OnStart the entry was already found (err %v), want ErrNotRunning", err)
	}
	if _, err := live.Running(); err != nil {
		t.Fatalf("after Ready the entry is missing: %v", err)
	}
}
