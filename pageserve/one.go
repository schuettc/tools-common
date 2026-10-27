package pageserve

// The one-entry case: a tool with one page (diazo, sift, cull, casebook)
// runs one serve per machine and finds it through live/serve.json, where its
// advert has always been. Moved from cull's internal/cull/serve/advert.go.

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	tools "github.com/subaud-tools/tools-common"
)

// ServeRoom is a one-page tool's entry: live/serve.json.
const ServeRoom = "serve"

// ErrNotRunning means no live serve on this machine. Running wraps it with the
// tool's name: "cull serve is not running".
var ErrNotRunning = errors.New("serve is not running")

// ServePath is the one-page tool's entry file. It is 0600 in a 0700
// directory, because it carries the token a same-user process authenticates
// with (the X-Local-Token header).
func (l Live) ServePath() string { return filepath.Join(l.Dir, ServeRoom+".json") }

// Advertise writes e as the tool's one entry. Unlike Write it does not
// validate: a one-page entry has no page, and its readers only ever ask
// Running for it.
func (l Live) Advertise(e Entry) error {
	e.V, e.Room = Version, ServeRoom
	return l.write(e)
}

// Withdraw removes the one entry when pid wrote it, so a serve that lost the
// race to start never removes the winner's.
func (l Live) Withdraw(pid int) {
	if e, err := l.readServe(); err == nil && e.PID == pid {
		_ = os.Remove(l.ServePath())
	}
}

func (l Live) readServe() (Entry, error) {
	var e Entry
	b, err := os.ReadFile(l.ServePath())
	if err != nil {
		return e, err
	}
	return e, json.Unmarshal(b, &e)
}

// Running returns the live serve's entry. A stale entry (dead PID) is
// removed and reported as not running.
func (l Live) Running() (Entry, error) {
	e, err := l.readServe()
	if errors.Is(err, fs.ErrNotExist) {
		return e, l.notRunning()
	}
	if err != nil {
		return e, err
	}
	if !tools.PIDAlive(e.PID) {
		_ = os.Remove(l.ServePath())
		return e, l.notRunning()
	}
	return e, nil
}

func (l Live) notRunning() error { return fmt.Errorf("%s %w", l.Tool, ErrNotRunning) }
