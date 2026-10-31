package pageserve

// The session's Chrome window: a session running in tmux has a terminal and,
// through proj, a Chrome window beside it, and a page it opens goes there. Moved
// from diazo's internal/serve/terminal.go and internal/channel/tools.go, with
// the plan's owner as a Where. Anything in proj is used as it is.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"

	"github.com/subaud-tools/tools-common/localweb"
)

// Where is where a session runs in tmux. It is zero outside tmux. A tool's
// route that takes it from its channel embeds it in the body it decodes.
type Where struct {
	Socket string `json:"tmux_socket,omitempty"`
	Pane   string `json:"tmux_pane,omitempty"`
}

// WhereFrom reads a session's Where from its environment: the socket's path is
// TMUX up to its first comma, the pane TMUX_PANE. It is zero unless both are
// there.
func WhereFrom(getenv func(string) string) Where {
	socket, _, _ := strings.Cut(getenv("TMUX"), ",")
	pane := getenv("TMUX_PANE")
	if socket == "" || pane == "" {
		return Where{}
	}
	return Where{Socket: socket, Pane: pane}
}

// Into adds w to the body of a call to serve, and nothing outside tmux, so a
// serve that predates the fields still takes the call.
func (w Where) Into(body map[string]any) {
	if w != (Where{}) {
		body["tmux_socket"], body["tmux_pane"] = w.Socket, w.Pane
	}
}

// TerminalState is where a session's tmux session stands, as proj terminal
// reads it: a terminal attached, running with none, or gone.
type TerminalState string

const (
	TerminalAttached TerminalState = "attached"
	TerminalDetached TerminalState = "detached"
	TerminalEnded    TerminalState = "ended"
)

// Values lists the states, for the wire's union.
func (TerminalState) Values() []string {
	return []string{string(TerminalAttached), string(TerminalDetached), string(TerminalEnded)}
}

// Terminal is proj terminal's answer for a session. Window says the session has
// a Chrome window, whichever terminal shows it; Chrome says that window's page
// is the view that shows it.
type Terminal struct {
	State   TerminalState `json:"state"`
	Session string        `json:"session,omitempty"`
	Chrome  bool          `json:"chrome,omitempty"`
	Window  bool          `json:"window,omitempty"`
}

// How long proj may take to read a state, and to show or open a terminal
// (opening waits for the new window to attach).
const (
	terminalRead = 5 * time.Second
	terminalGo   = 30 * time.Second
)

// Proj runs proj with args and returns its stdout, with stderr added when it
// fails, and its exit code; err when it could not run at all. A test replaces it
// (a tool's too, for its own routes on these calls).
var Proj = func(ctx context.Context, args ...string) ([]byte, int, error) {
	out, err := exec.CommandContext(ctx, "proj", args...).Output()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return append(out, ee.Stderr...), ee.ExitCode(), nil
	}
	if err != nil {
		return out, -1, err
	}
	return out, 0, nil
}

// TerminalOf runs proj terminal for the session at w. It is nil, with no error,
// outside tmux, which a w missing its socket or its pane is too; an error says
// why proj gave no state.
func TerminalOf(w Where) (*Terminal, error) { return terminal(w, false) }

// TerminalGo is TerminalOf with --go: proj shows the session in a terminal, or
// opens one, and this is the state after it.
func TerminalGo(w Where) (*Terminal, error) { return terminal(w, true) }

func terminal(w Where, goTo bool) (*Terminal, error) {
	if w.Socket == "" || w.Pane == "" {
		return nil, nil
	}
	limit := terminalRead
	args := []string{"terminal", "--socket", w.Socket, "--pane", w.Pane}
	if goTo {
		limit = terminalGo
		args = append(args, "--go")
	}
	ctx, cancel := context.WithTimeout(context.Background(), limit)
	defer cancel()
	out, code, err := Proj(ctx, append(args, "--json")...)
	switch {
	case err != nil:
		return nil, err
	case code != 0:
		return nil, fmt.Errorf("proj terminal exited %d: %s", code, strings.TrimSpace(string(out)))
	}
	var t Terminal
	if err := json.Unmarshal(out, &t); err != nil {
		return nil, fmt.Errorf("proj terminal: %w", err)
	}
	if !slices.Contains(t.State.Values(), string(t.State)) {
		return nil, fmt.Errorf("proj terminal: unknown state %q", t.State)
	}
	return &t, nil
}

// Window runs proj window for the session at w, which opens the session's Chrome
// window or brings it forward, with url on its right half; with keepView a
// terminal showing the session elsewhere keeps it. An error carries what proj
// said.
func Window(w Where, url string, keepView bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), terminalGo)
	defer cancel()
	args := []string{"window", "--socket", w.Socket, "--pane", w.Pane, "--right", url}
	if keepView {
		args = append(args, "--keep-view")
	}
	out, code, err := Proj(ctx, append(args, "--json")...)
	switch {
	case err != nil:
		return err
	case code != 0:
		return fmt.Errorf("proj window exited %d: %s", code, strings.TrimSpace(string(out)))
	}
	return nil
}

// Show's answers: the session's Chrome window took the page, a page tab did, or
// none is open and the caller opens the browser.
const (
	ShowWindow = "window"
	ShowReused = "reused"
	ShowOpened = "opened"
)

// showLog is where Show says why proj gave no window: stderr, which is serve's
// log. Tests replace it.
var showLog io.Writer = os.Stderr

// Show puts page, at url, in the Chrome window of the session at w when it has
// one, and otherwise in a tab: one that shows it, a front-page tab asked through
// events to move to it, or tabs on other pages, which list it in their bar. It
// opens nothing itself: on ShowOpened the caller's channel opens the browser. A
// proj that fails costs the window only, and is logged.
func Show(w Where, url string, events *Events, page string) string {
	if showInWindow(w, url) {
		return ShowWindow
	}
	c := events.Tabs.Pick(page)
	switch c.Kind {
	case localweb.TabNone:
		return ShowOpened
	case localweb.TabMove:
		events.Tabs.Moved(c.Tab, page)
		events.Emit("show", map[string]string{"tab": c.Tab, "page": page})
	}
	return ShowReused
}

func showInWindow(w Where, url string) bool {
	t, err := TerminalOf(w)
	if err == nil && t != nil && t.Window {
		err = Window(w, url, true)
		if err == nil {
			return true
		}
	}
	if err != nil {
		_, _ = fmt.Fprintf(showLog, "the session's window: %v\n", err)
	}
	return false
}
