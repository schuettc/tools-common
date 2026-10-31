package pageserve

// The channel: the MCP server each agent session starts (Claude Code through
// the tool's plugin, pi through channels.tools). It gives the agent the
// tool's tools, and a tool call starts serve when it isn't running. Under pi
// it registers the session's presence, holds a long poll open for the human's
// Sends and injects each one as a notifications/claude/channel event; that
// loop never starts serve. Under Claude Code
// `<tool> watch` runs that loop instead and prints each Send as a JSON line
// for the plugin's mod. Moved from cull's internal/cull/channel/channel.go.
//
// stdout carries MCP frames only; every log line goes to the Log writer
// (stderr in the command).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/subaud-tools/tools-common/channelmcp"
	"github.com/subaud-tools/tools-common/harness"
	"github.com/subaud-tools/tools-common/localweb"
)

// Spec is what a tool's channel is made of: its tools, and how it reads its
// serve's answers. Everything else is the same in every tool.
type Spec struct {
	Tool         string // the tool's name, as its MCP server and in logs
	Version      string
	Instructions string // the standing rules the agent gets with the channel
	Tools        []channelmcp.Tool
	// Call runs one of Tools for the agent.
	Call func(ctx context.Context, ch *Channel, name string, args json.RawMessage) (string, error)
	// Root resolves the session's project from its folder: the loop runs only
	// when it does, and presence and wait name the session's folder as the
	// root its Sends come from. nil: the tool has no project, and they name
	// no root.
	Root func(cwd string) (string, error)
	// Wake reads one answer of GET /api/agent/wait into the event pushed to
	// the agent; ok is false when there is nothing to push (the wait timed
	// out).
	Wake func(body json.RawMessage) (text string, meta map[string]string, ok bool)
	// Answers names what the human sends, in the channel's log lines:
	// "Court's answers".
	Answers string
	// NoPresence: serve has no POST /api/agent/presence (diazo's), so the
	// loop only waits.
	NoPresence bool
	// PresenceBody is the body of POST /api/agent/presence for id (nil:
	// {session, harness, label, root}, root left out when the tool has no
	// project).
	PresenceBody func(id Identity) any
	// OnServe runs in the wait loop when the serve it reaches is a new
	// process (its entry's StartedAt changed since the loop last looked),
	// not for the first serve the loop finds; the loop then registers
	// presence with the new serve. A tool that tells the agent what a restart
	// interrupted asks serve here, through ch.Client.Passive(), and pushes
	// with ch.Push. nil: the loop doesn't watch for a new serve.
	OnServe func(ctx context.Context, ch *Channel, e Entry)
}

// Identity is the session a channel serves.
type Identity struct {
	Session string
	Harness string // "claude" or "pi"
	Label   string
	CWD     string
	// PID is the harness's process (the channel's parent: Claude Code and
	// pi's channels.tools start the channel directly). Sessions in one pi
	// process share it, which tells a pi-subagents worker from a fork.
	PID int
}

// FromEnv resolves the session by the family rule (tools-common harness).
// Session is "" outside a harness.
func FromEnv() Identity {
	c := harness.FromEnv()
	id := Identity{Session: c.SessionID, CWD: c.CWD, Harness: "pi", PID: os.Getppid()}
	if c.IsClaude() {
		id.Harness = "claude"
	}
	if id.CWD == "" {
		id.CWD, _ = os.Getwd()
	}
	id.Label = id.Harness + " · " + filepath.Base(id.CWD)
	return id
}

// Channel is a running channel.
type Channel struct {
	Spec   Spec
	ID     Identity
	Client *Client
	Server *channelmcp.Server
	Log    io.Writer
	// Open opens a URL in the human's browser (tests replace it).
	Open func(url string) error
	// Retry is how long to wait before trying serve again; Presence how often
	// to refresh presence; Poll the long-poll timeout. Tests shorten them.
	Retry    time.Duration
	Presence time.Duration
	Poll     time.Duration

	// Push is where the wait loop sends each Send: Server for `<tool>
	// channel`, a JSON line on stdout for `<tool> watch`. One loop, two
	// outputs.
	Push channelmcp.Pusher
	// ServeOnly is `<tool> channel` under Claude Code: the plugin's mod runs
	// `<tool> watch`, which takes the sends, so this process serves the tools
	// and never waits. Two takers would split a session's sends.
	ServeOnly bool

	root    string // the session's project root; "" when it can't be resolved
	rootErr error
	runCtx  context.Context
	reports chan report // pi's reports, posted to serve in order by one goroutine
}

// queueReport hands a report to the goroutine that posts them. A full queue
// drops it, logged without its token: the token is the harness's delivery
// claim, not something a log should carry.
func (ch *Channel) queueReport(event channelmcp.ReportEvent, token string) {
	select {
	case ch.reports <- report{event, token}:
	default:
		ch.logf("dropped report %s: too many waiting", event)
	}
}

// report is one report from the harness, waiting to be posted to serve.
type report struct {
	event channelmcp.ReportEvent
	token string
}

// New builds a channel for id. The project root is spec.Root of the
// session's working directory.
func New(spec Spec, id Identity, c *Client) *Channel {
	ch := &Channel{Spec: spec, ID: id, Client: c, Log: io.Discard, Open: localweb.OpenBrowser,
		Retry: 5 * time.Second, Presence: 30 * time.Second, Poll: 55 * time.Second, reports: make(chan report, 64)}
	if spec.Root != nil {
		ch.root, ch.rootErr = spec.Root(id.CWD)
	}
	ch.Server = channelmcp.New(channelmcp.Handler{
		Name: spec.Tool, Version: spec.Version, Instructions: spec.Instructions, Tools: spec.Tools,
		Call: func(name string, args json.RawMessage) (string, error) {
			ctx := ch.runCtx
			if ctx == nil {
				ctx = context.Background()
			}
			// A tool call is where a stale serve gets replaced (the wait loop
			// never does it), so no tool has to ask.
			if err := c.Fresh(ctx); err != nil {
				return "", err
			}
			return spec.Call(ctx, ch, name, args)
		},
		// Run's read loop hands each report over and goes on: posting it
		// to serve waits on the network, and tool calls must not.
		OnReport: ch.queueReport,
	})
	ch.Push = ch.Server
	return ch
}

// ForSession is the channel `<tool> channel` runs: this session, and
// serve-only when the session is Claude Code's (see ServeOnly).
func ForSession(spec Spec, c *Client) *Channel {
	ch := New(spec, FromEnv(), c)
	ch.ServeOnly = harness.FromEnv().IsClaude()
	return ch
}

// Root is the session's project root, or why it has none (spec.Root's
// answer; "" and nil without one).
func (ch *Channel) Root() (string, error) { return ch.root, ch.rootErr }

func (ch *Channel) logf(format string, args ...any) {
	_, _ = fmt.Fprintf(ch.Log, "%s channel: %s\n", ch.Spec.Tool, fmt.Sprintf(format, args...))
}

// scope is the session's folder: the session covers the repository that
// contains it and every repository inside it (a workspace root), so a Send
// for any of them reaches this session. "" when the tool has no project.
func (ch *Channel) scope() string {
	if ch.Spec.Root == nil {
		return ""
	}
	if abs, err := filepath.Abs(ch.ID.CWD); err == nil {
		return abs
	}
	return ch.root
}

// Run serves MCP on r/w and, unless the channel is serve-only, the presence
// and wake loop when the session (and its project) are known.
func (ch *Channel) Run(ctx context.Context, r io.Reader, w io.Writer) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	ch.runCtx = ctx // set before Server.Run starts reading
	if ch.ServeOnly {
		ch.logf("Claude Code session: serving tools only; the %s plugin's `%s watch` delivers %s", ch.Spec.Tool, ch.Spec.Tool, ch.Spec.Answers)
	} else if err := ch.ready(); err != nil {
		ch.logf("%v", err)
	} else {
		go ch.loop(ctx)
		go ch.postReports(ctx)
	}
	return ch.Server.Run(r, w)
}

// postReports posts each report the harness sent to serve, in the order
// sent, until ctx ends. It never starts serve. A failed post is logged, not
// retried.
func (ch *Channel) postReports(ctx context.Context) {
	c := ch.Client.Passive()
	for {
		select {
		case <-ctx.Done():
			return
		case r := <-ch.reports:
			if err := c.Report(ctx, ch.ID.Session, r.event, r.token); err != nil && ctx.Err() == nil {
				ch.logf("report %s: %v", r.event, err)
			}
		}
	}
}

// Watch is `<tool> watch`: the presence and wake loop alone, until ctx ends.
func (ch *Channel) Watch(ctx context.Context) error {
	if err := ch.ready(); err != nil {
		return err
	}
	ch.loop(ctx)
	return nil
}

// ready says why the loop cannot run: no session, or no project.
func (ch *Channel) ready() error {
	if ch.ID.Session == "" {
		return fmt.Errorf("no session id (not under Claude Code or pi); %s will not reach this session", ch.Spec.Answers)
	}
	return ch.rootErr
}

// Announce registers the session's presence with serve through c: the
// loop's passive client, or a tool call's, which starts serve (a tool that
// re-registers when serve answers that it doesn't know the session).
func (ch *Channel) Announce(ctx context.Context, c *Client) error {
	var body any
	if ch.Spec.PresenceBody != nil {
		body = ch.Spec.PresenceBody(ch.ID)
	} else {
		b := map[string]any{"session": ch.ID.Session, "harness": ch.ID.Harness, "label": ch.ID.Label}
		if root := ch.scope(); root != "" {
			b["root"] = root
		}
		body = b
	}
	_, err := c.Do(ctx, http.MethodPost, "/api/agent/presence", body, nil)
	return err
}

// loop keeps presence fresh and long-polls for sends, emitting each claimed
// send as one channel event. The next poll is armed as soon as an event is
// queued (Notify only queues), so nothing sent meanwhile is missed. serve
// claims a send for exactly one caller, so an event is never duplicated. A
// delivery the tool's Wake refuses is logged with its first 200 bytes, so a
// lost one leaves a trace. It
// never starts serve (only a tool call does), and while no serve runs it logs
// nothing.
func (ch *Channel) loop(ctx context.Context) {
	c := ch.Client.Passive()
	var lastPresence time.Time
	var lastErr string
	var seen time.Time // StartedAt of the serve the loop last reached (with OnServe)
	logOnce := func(err error) {
		if errors.Is(err, ErrNoServe) {
			return
		}
		if msg := err.Error(); msg != lastErr {
			ch.logf("%s", msg)
			lastErr = msg
		}
	}
	for ctx.Err() == nil {
		if ch.Spec.OnServe != nil {
			if e, err := c.Find(); err == nil && !e.StartedAt.Equal(seen) {
				if !seen.IsZero() {
					ch.Spec.OnServe(ctx, ch, e)
				}
				seen, lastPresence = e.StartedAt, time.Time{}
			}
		}
		if !ch.Spec.NoPresence && time.Since(lastPresence) > ch.Presence {
			if err := ch.Announce(ctx, c); err != nil {
				if ctx.Err() != nil {
					return
				}
				logOnce(err)
				sleep(ctx, ch.Retry)
				continue
			}
			lastPresence, lastErr = time.Now(), ""
		}
		var got json.RawMessage
		code, err := c.Do(ctx, http.MethodGet, "/api/agent/wait?"+Query(
			"session", ch.ID.Session, "root", ch.scope(), "timeout", strconv.Itoa(max(int(ch.Poll.Seconds()), 1))), nil, &got)
		switch {
		case err != nil:
			if ctx.Err() != nil || errors.Is(err, context.Canceled) {
				return
			}
			var se *StatusError
			if errors.As(err, &se) {
				lastPresence = time.Time{}
			}
			logOnce(err)
			sleep(ctx, ch.Retry)
		case code == http.StatusOK && len(got) > 0:
			lastErr = ""
			text, meta, ok := ch.Spec.Wake(got)
			if !ok {
				ch.logf("refused delivery: %s", refused(got))
				continue
			}
			if err := ch.Push.Notify(text, meta); err != nil {
				ch.logf("notify: %v", err)
			}
		default:
			lastErr = ""
		}
	}
}

// refused describes a delivery the tool's Wake refused by its size and its
// top-level field names, never its values: they carry Court's words.
func refused(body json.RawMessage) string {
	var fields map[string]json.RawMessage
	if json.Unmarshal(body, &fields) != nil {
		return fmt.Sprintf("%d bytes, not a JSON object", len(body))
	}
	names := slices.Sorted(maps.Keys(fields))
	return fmt.Sprintf("%d bytes, fields %s", len(body), strings.Join(names, " "))
}

func sleep(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}
