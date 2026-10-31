// Command scratch is a server built only from pageserve, with none of its own
// code for the server rules, which conformance must pass. SCRATCH_BREAK names
// a rule to break, the one thing it does beyond that, which conformance must
// fail: that is what proves the check can fail.
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"testing/fstest"
	"time"

	tools "github.com/subaud-tools/tools-common"
	"github.com/subaud-tools/tools-common/localweb"
	"github.com/subaud-tools/tools-common/pageserve"
)

const tool = "scratch"

var assets = fstest.MapFS{"index.html": {Data: []byte("<!doctype html><title>scratch</title>")}}

func main() {
	broke := os.Getenv("SCRATCH_BREAK")
	var err error
	switch {
	case len(os.Args) > 1 && os.Args[1] == "serve":
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		err = serve(ctx, broke)
	case len(os.Args) > 1 && os.Args[1] == "skills" && broke != "skills":
		skill := func() ([]byte, error) { return []byte("---\nname: scratch\n---\nA skill.\n"), nil }
		err = pageserve.SkillsCommand(tool, skill).Run(os.Args[2:], os.Stdout, os.Stderr)
	default:
		err = fmt.Errorf("unknown command: %v", os.Args[1:])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func serve(ctx context.Context, broke string) error {
	state := tools.StateDir(tool)
	if err := os.MkdirAll(state, 0o700); err != nil {
		return err
	}
	o := pageserve.Options{Live: pageserve.LiveFor(tool), Assets: assets, Version: "1.2.3"}
	switch broke {
	case "token": // a tool that makes a new token each start
		_ = os.Remove(filepath.Join(state, "token"))
	case "idle": // a tool with its own idle exit
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 500*time.Millisecond)
		defer cancel()
	case "stale": // a tool that runs its own server and never watches its binary
		return handRolled(ctx, o)
	case "tabs": // a list that can't be kept
		if err := os.Mkdir(filepath.Join(state, "tabs.json"), 0o700); err != nil {
			return err
		}
	case "stop": // a tool whose own server takes the stop with the cookie
		return cookieStop(ctx, o)
	case "build": // a tool route that answers without the header
		o.API = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Del(pageserve.BuildHeader)
			w.WriteHeader(http.StatusNotFound)
		})
	}
	return pageserve.Run(ctx, o)
}

// cookieStop serves and advertises itself with a stop route anything signed in
// can reach.
func cookieStop(ctx context.Context, o pageserve.Options) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/stop", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
		go cancel()
	})
	srv, err := localweb.Start(ctx, localweb.Config{Tool: tool, Assets: o.Assets, API: mux})
	if err != nil {
		return err
	}
	e := pageserve.Entry{URL: srv.URL, Base: "http://" + srv.Addr(), Token: srv.Token, PID: os.Getpid()}
	if err := o.Live.Advertise(e); err != nil {
		return err
	}
	defer o.Live.Withdraw(e.PID)
	return srv.Wait()
}

// handRolled serves and advertises itself without pageserve.Run.
func handRolled(ctx context.Context, o pageserve.Options) error {
	srv, err := localweb.Start(ctx, localweb.Config{Tool: tool, Assets: o.Assets})
	if err != nil {
		return err
	}
	e := pageserve.Entry{URL: srv.URL, Base: "http://" + srv.Addr(), Token: srv.Token, PID: os.Getpid()}
	if err := o.Live.Advertise(e); err != nil {
		return err
	}
	defer o.Live.Withdraw(e.PID)
	return srv.Wait()
}
