package pageserve

// The stale mark: serve notices a newly installed binary. Moved from diazo's
// internal/serve/watch.go.

import (
	"context"
	"os"
	"time"
)

// executable is the binary Run watches; tests replace it.
var executable = Executable

// staleEvery is how often Run looks at its binary; tests shorten it.
var staleEvery = time.Second

// binaryWatch is what the stale check keeps between ticks.
type binaryWatch struct {
	exe    string
	was    os.FileInfo // exe as it was when serve started
	live   Live
	events *Events
	stale  bool // the entry says so, and the event went out
}

// watchBinary records the binary serve runs, so check can tell when an
// install replaces it.
func watchBinary(exe string, live Live, events *Events) (*binaryWatch, error) {
	st, err := os.Stat(exe)
	if err != nil {
		return nil, err
	}
	return &binaryWatch{exe: exe, was: st, live: live, events: events}, nil
}

// run checks every `every` until ctx ends.
func (b *binaryWatch) run(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			b.check()
		}
	}
}

// check marks serve stale once its binary is replaced (an install writes a new
// file, so the inode changes): its entry says so for the next tool call, which
// restarts serve through Client.Fresh, and a stale event tells every page. A
// running serve keeps its old code, so without this a page silently stays on
// the old build.
//
// STALE IS RECORDED ONLY AFTER THE ENTRY IS REWRITTEN. A tick can run before
// the entry is written; setting the flag first would use the install up and
// leave the entry unmarked for good, so a failed mark is tried again next tick.
func (b *binaryWatch) check() {
	if b.stale {
		return
	}
	now, err := os.Stat(b.exe)
	if err != nil {
		return // mid-install the file may briefly be missing
	}
	// Linux can hand a replaced file its old inode number, so the same inode
	// alone isn't the same build: its size or time changes too.
	if os.SameFile(b.was, now) && now.Size() == b.was.Size() && now.ModTime().Equal(b.was.ModTime()) {
		return
	}
	// Only this serve's own entry is marked.
	e, err := b.live.Running()
	if err != nil || e.PID != os.Getpid() {
		return
	}
	e.Stale = true
	if b.live.Advertise(e) != nil {
		return
	}
	b.stale = true
	b.events.Emit("stale", struct{}{})
}
