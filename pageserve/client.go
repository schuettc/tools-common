package pageserve

// The client a channel calls serve with: find the live serve through its
// entry, authenticate with its token, optionally start it. Moved from cull's
// internal/cull/channel/client.go, with diazo's Fresh.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	tools "github.com/subaud-tools/tools-common"
	"github.com/subaud-tools/tools-common/channelmcp"
	"github.com/subaud-tools/tools-common/localweb"
)

// Client talks to the tool's live serve with its entry's token.
type Client struct {
	Tool string
	HTTP *http.Client
	// Find locates serve; tests replace it.
	Find func() (Entry, error)
	// Start starts serve when Find says it isn't running (nil: don't).
	Start func() (Entry, error)
	// Stop stops the live serve; tests replace it.
	Stop func(context.Context) error
	// Lock serializes replacing a stale serve across every channel on the
	// machine; nil: within this client only.
	Lock func() (unlock func(), err error)

	// startMu serializes "find, else start" so one channel never launches two
	// serves; Process.Ensure serializes it across every channel on the machine.
	startMu sync.Mutex
}

// NewClient returns a client that finds serve through l's entry and, when
// start is not nil, starts it if it isn't running.
func NewClient(l Live, start func() (Entry, error)) *Client {
	return &Client{Tool: l.Tool, HTTP: &http.Client{Timeout: 90 * time.Second}, Find: l.Running, Start: start,
		Stop: func(ctx context.Context) error { return Stop(ctx, l) },
		Lock: func() (func(), error) {
			state := tools.StateDir(l.Tool)
			if err := os.MkdirAll(state, 0o700); err != nil {
				return nil, err
			}
			return lockFile(filepath.Join(state, "fresh.lock"))
		}}
}

// Passive is a copy of c that never starts serve: the client the channel's
// wait loop, its reports, `<tool> watch` and `<tool> report` use. Only a tool
// call starts serve.
func (c *Client) Passive() *Client {
	return &Client{Tool: c.Tool, HTTP: c.HTTP, Find: c.Find, Stop: c.Stop, Lock: c.Lock}
}

// errStart is a failed start, wrapped as "starting cull serve: ...".
var errStart = errors.New("starting")

func (c *Client) ensure() (Entry, error) {
	c.startMu.Lock()
	defer c.startMu.Unlock()
	e, err := c.Find()
	if err != nil && c.Start != nil {
		if e, err = c.Start(); err != nil {
			return e, fmt.Errorf("%w %s serve: %w", errStart, c.Tool, err)
		}
	}
	return e, err
}

// Fresh replaces a serve that marked itself stale (a newer binary was
// installed since it started) with one running the installed binary. The
// channel calls it before each tool call, never in the middle of a wait: serve
// keeps its token, so open tabs reconnect, but each reloads for the new page
// (or asks to).
func (c *Client) Fresh(ctx context.Context) error {
	if c.Start == nil {
		return nil
	}
	c.startMu.Lock()
	e, err := c.Find()
	c.startMu.Unlock()
	switch {
	case errors.Is(err, ErrNotRunning):
		return nil // the next call starts one from the installed binary
	case err != nil:
		return err
	case !e.Stale:
		return nil
	}
	// Channels finding it stale at once replace it once: holding the lock, look
	// again, because the serve there now may be the one another just started.
	if c.Lock != nil {
		unlock, err := c.Lock()
		if err != nil {
			return err
		}
		defer unlock()
	}
	c.startMu.Lock()
	e, err = c.Find()
	c.startMu.Unlock()
	switch {
	case errors.Is(err, ErrNotRunning): // another channel stopped it: start one
		_, err = c.ensure()
		return err
	case err != nil:
		return err
	case !e.Stale:
		return nil
	}
	if err := c.Stop(ctx); err != nil && !errors.Is(err, ErrNotRunning) {
		return fmt.Errorf("restarting %s serve for the newer install: %w", c.Tool, err)
	}
	_, err = c.ensure()
	return err
}

// ErrNoServe means the tool's serve isn't running on this machine (and this
// client doesn't start it). Do wraps it with the tool's name: "cull serve
// isn't running".
var ErrNoServe = errors.New("serve isn't running")

// StatusError is a non-2xx answer from serve. ErrCode is the JSON body's
// "code" when it has one ("unknown_session"), so a caller can tell two
// answers with one status apart; "" without one.
type StatusError struct {
	Tool    string
	Code    int
	Msg     string
	ErrCode string
}

func (e *StatusError) Error() string { return fmt.Sprintf("%s serve: %d %s", e.Tool, e.Code, e.Msg) }

// Do calls serve: method, path (with query), a JSON body (nil for none), and
// decodes a JSON answer into out (nil to ignore). It returns the status code;
// 204 leaves out untouched.
func (c *Client) Do(ctx context.Context, method, path string, body, out any) (int, error) {
	e, err := c.ensure()
	if errors.Is(err, errStart) {
		return 0, err
	}
	if err != nil {
		return 0, fmt.Errorf("%s %w", c.Tool, ErrNoServe)
	}
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, e.Base+path, rd)
	if err != nil {
		return 0, err
	}
	req.Header.Set(localweb.TokenHeader, e.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return 0, fmt.Errorf("%s %w (%w)", c.Tool, ErrNoServe, err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode >= 300 {
		var e struct {
			Error string `json:"error"`
			Code  string `json:"code"`
		}
		_ = json.Unmarshal(b, &e)
		if e.Error == "" {
			e.Error = string(bytes.TrimSpace(b))
		}
		return resp.StatusCode, &StatusError{Tool: c.Tool, Code: resp.StatusCode, Msg: e.Error, ErrCode: e.Code}
	}
	if out != nil && resp.StatusCode != http.StatusNoContent && len(b) > 0 {
		if err := json.Unmarshal(b, out); err != nil {
			return resp.StatusCode, err
		}
	}
	return resp.StatusCode, nil
}

// Report hands serve one report from the harness about session: a send
// held or landed (with its claim's token), or the turn ended.
func (c *Client) Report(ctx context.Context, session string, event channelmcp.ReportEvent, token string) error {
	_, err := c.Do(ctx, http.MethodPost, "/api/agent/report", map[string]string{"session": session, "event": string(event), "token": token}, nil)
	return err
}

// Query encodes pairs (key, value, ...) as a URL query, leaving out each
// empty value.
func Query(pairs ...string) string {
	v := url.Values{}
	for i := 0; i+1 < len(pairs); i += 2 {
		if pairs[i+1] != "" {
			v.Set(pairs[i], pairs[i+1])
		}
	}
	return v.Encode()
}
