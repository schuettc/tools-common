package pageserve

// The serve process: one local server per machine that serves the tool's
// page and API, streams its events, and writes the entry other processes find
// (and stop) it by. Moved from cull's internal/cull/serve/server.go, with
// diazo's serve.lock and kept token (token.go).

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"time"

	tools "github.com/subaud-tools/tools-common"
	"github.com/subaud-tools/tools-common/localweb"
)

// BuildHeader is on every API response: the version and a hash of the page's
// assets, so an open page sees serve change (the kit's createApi, onNewBuild).
const BuildHeader = "X-Tool-Build"

// buildOf is what BuildHeader says, "<version>+<hash>", walked from assets so a
// rebuild with the same version still counts as a change.
func buildOf(version string, assets fs.FS) string {
	h := sha256.New()
	if assets != nil {
		_ = fs.WalkDir(assets, ".", func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			b, _ := fs.ReadFile(assets, path)
			// Each name and length goes in, so a file's bounds count.
			_, _ = fmt.Fprintf(h, "%s\x00%d\x00", path, len(b))
			h.Write(b)
			return nil
		})
	}
	return version + "+" + hex.EncodeToString(h.Sum(nil))[:12]
}

// Options configures Run.
type Options struct {
	Live   Live         // where the entry is written; Live.Tool names the tool
	Assets fs.FS        // the page, served at / without a token
	API    http.Handler // the tool's own /api/ routes (token required); nil: none
	// Events is the log behind /api/events and /api/poll (nil: a new one).
	Events *Events
	// Tabs, when set, is handed the list of the page's open tabs before the
	// server listens, for the tool's handlers to Pick from. Run makes the list,
	// keeps it in StateDir/tabs.json and hears from it through Events, whether
	// this is set or not, so a tool makes none of its own.
	Tabs    func(*localweb.Tabs)
	Port    int // 0: remembered, else free
	Version string
	Log     io.Writer // one line per start/stop
	// OnStart runs the tool's own start-up steps once the server listens and
	// before its entry is written, so whoever finds the entry finds them
	// done; it gets the server's context and the page's URL, and anything it
	// starts in the background must end with ctx.
	OnStart func(ctx context.Context, url string)
	Ready   func(url string)
	// Stream, when set, answers GET /api/events instead of Events.Stream:
	// a tool whose page streams from its own event bus (casebook's).
	// GET /api/poll stays Events'.
	Stream http.HandlerFunc
	// Entry, when set, edits the entry just before it is written: the
	// server's own start time as StartedAt, Reopened. The edited entry must
	// still validate, or Run fails.
	Entry func(*Entry)
}

// Run serves until ctx ends or POST /api/stop; it never exits for being idle.
// It writes the tool's one entry (Live.Advertise) while running and removes it
// on the way out, keeps its token in StateDir/token across restarts, so open
// tabs keep working, and marks itself stale once its binary is replaced (see
// binaryWatch), which the next tool call acts on. Every API response carries
// BuildHeader. Besides the tool's API it answers GET /api/events (Options.Stream, else
// Events.Stream), GET /api/poll and POST /api/stop.
func Run(ctx context.Context, o Options) error {
	if o.Log == nil {
		o.Log = io.Discard
	}
	if o.Events == nil {
		o.Events = NewEvents()
	}
	tool := o.Live.Tool
	state := tools.StateDir(tool)
	if err := os.MkdirAll(state, 0o700); err != nil {
		return err
	}
	// One serve per machine: from finding no live entry to writing its own,
	// a serve holds StateDir/serve.lock, so of serves started at once the
	// first serves and the rest find its entry and exit.
	unlock, err := lockFile(filepath.Join(state, "serve.lock"))
	if err != nil {
		return err
	}
	locked := true
	release := func() {
		if locked {
			locked = false
			unlock()
		}
	}
	defer release()
	if e, err := o.Live.Running(); err == nil {
		return fmt.Errorf("%s serve is already running (pid %d, %s)", tool, e.PID, e.Base)
	}
	token, err := keptToken(state)
	if err != nil {
		return err
	}
	// The binary as it is now, before anything can be found, so an install
	// from here on counts.
	binary, watchErr := watchBinary(executable(), o.Live, o.Events)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	o.Events.bind(ctx)
	// The open tabs outlive a restart, so the first page open after an install
	// reuses a tab instead of opening one (a hidden tab checks in only once a
	// minute).
	if o.Events.Tabs == nil {
		o.Events.Tabs = localweb.NewTabs(nil)
	}
	if err := o.Events.Tabs.Keep(filepath.Join(state, "tabs.json")); err != nil {
		_, _ = fmt.Fprintf(o.Log, "%s serve: the open tabs were not read: %v\n", tool, err)
	}
	if o.Tabs != nil {
		o.Tabs(o.Events.Tabs)
	}
	mux := http.NewServeMux()
	stream := o.Events.Stream
	if o.Stream != nil {
		stream = o.Stream
	}
	mux.HandleFunc("GET /api/events", stream)
	mux.HandleFunc("GET /api/poll", o.Events.Poll)
	mux.HandleFunc("POST /api/stop", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
		go func() { time.Sleep(50 * time.Millisecond); cancel() }()
	})
	if o.API != nil {
		mux.Handle("/", o.API)
	}
	build := buildOf(o.Version, o.Assets)
	api := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(BuildHeader, build)
		mux.ServeHTTP(w, r)
	})
	srv, err := localweb.Start(ctx, localweb.Config{Tool: tool, Assets: o.Assets, API: api, Port: o.Port, Token: token})
	if err != nil {
		return err
	}
	// THE TOOL IS SET UP BEFORE IT CAN BE FOUND. A channel that reads the
	// entry calls the tool's API at once, so OnStart (cull sets its page URL
	// there) runs first; anything it starts ends with ctx if the write fails.
	if o.OnStart != nil {
		o.OnStart(ctx, srv.URL)
	}
	e := Entry{URL: srv.URL, Base: "http://" + srv.Addr(), Token: srv.Token, PID: os.Getpid(), Version: o.Version, StartedAt: time.Now().UTC()}
	if o.Entry != nil {
		o.Entry(&e)
	}
	if err := o.Live.Advertise(e); err != nil {
		cancel()
		_ = srv.Wait()
		return err
	}
	release()
	defer o.Live.Withdraw(e.PID)
	_, _ = fmt.Fprintf(o.Log, "%s serve: started %s (pid %d)\n", tool, e.Base, e.PID)
	defer func() { _, _ = fmt.Fprintf(o.Log, "%s serve: stopped (pid %d)\n", tool, e.PID) }()
	watched := make(chan struct{})
	if watchErr != nil {
		_, _ = fmt.Fprintf(o.Log, "%s serve: not watching its binary for a new install: %v\n", tool, watchErr)
		close(watched)
	} else {
		every := staleEvery
		go func() { defer close(watched); binary.run(ctx, every) }()
	}
	if o.Ready != nil {
		o.Ready(srv.URL)
	}
	err = srv.Wait()
	cancel()
	o.Events.wait() // streams end with ctx
	// Saved as serve stops, so the list a restart reads is not already old.
	if err := o.Events.Tabs.Save(); err != nil {
		_, _ = fmt.Fprintf(o.Log, "%s serve: the open tabs were not saved: %v\n", tool, err)
	}
	<-watched // a last tick must not write the entry after it is withdrawn
	return err
}
