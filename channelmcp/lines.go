package channelmcp

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"

	"github.com/subaud-tools/tools-common/harness"
)

// Pusher is where a tool's wait loop sends each event: a *Server for
// `<tool> channel` (an MCP notification), *Lines for `<tool> watch` (a JSON
// line a Claude Code mod reads). One loop serves both.
type Pusher interface {
	Notify(content string, meta map[string]string) error
}

var (
	_ Pusher = (*Server)(nil)
	_ Pusher = (*Lines)(nil)
)

// Lines writes each event as one JSON line to w,
// {"text":...,"content":...,"meta":{...}}: text is the whole prompt a Claude
// Code mod submits (see eventText); content and meta stay for mods rendered
// before 0.18.0, which build the same text themselves. The line format is
// additive: fields are added, never renamed or removed. Notify is safe from
// several goroutines: each line is written whole, under one mutex, so lines
// never interleave.
type Lines struct {
	mu   sync.Mutex
	w    io.Writer
	tool string
}

// NewLines returns a Pusher that prints to w with no text field (it has no
// tool name for the header). A tool's watch uses StartWatch instead.
func NewLines(w io.Writer) *Lines { return &Lines{w: w} }

// NewLinesFor returns a Pusher that prints to w with tool's name in each
// event's text.
func NewLinesFor(w io.Writer, tool string) *Lines { return &Lines{w: w, tool: tool} }

// Notify refuses the same meta keys Server.Notify refuses, so an event that
// works on one output works on the other. A nil meta prints as {}.
func (l *Lines) Notify(content string, meta map[string]string) error {
	if err := checkMeta(meta); err != nil {
		return err
	}
	if meta == nil {
		meta = map[string]string{}
	}
	var text string
	if l.tool != "" {
		text = eventText(l.tool, content, meta)
	}
	b, err := json.Marshal(struct {
		Text    string            `json:"text,omitempty"`
		Content string            `json:"content"`
		Meta    map[string]string `json:"meta"`
	}{text, content, meta})
	if err != nil {
		return err
	}
	b = append(b, '\n')
	l.mu.Lock()
	defer l.mu.Unlock()
	_, err = l.w.Write(b)
	return err
}

// eventText is the prompt for one event: "<tool> event", a " key=value" per
// meta pair in sorted key order, a newline, then content. It is exactly what
// the 0.17.0 mod builds from content and meta, so old and new mods submit the
// same words.
func eventText(tool, content string, meta map[string]string) string {
	keys := make([]string, 0, len(meta))
	for k := range meta {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString(tool + " event")
	for _, k := range keys {
		b.WriteString(" " + k + "=" + metaValue(meta[k]))
	}
	b.WriteString("\n" + content)
	return b.String()
}

// metaValue writes a value that is empty or holds whitespace, a quote or = as
// a JSON string, so it stays on the header line and reads as one value.
func metaValue(v string) string {
	if v != "" && !strings.ContainsFunc(v, func(r rune) bool { return r == '"' || r == '=' || jsSpace(r) }) {
		return v
	}
	return jsQuote(v)
}

// jsSpace is JavaScript's \s: the 0.17.0 mod tested values with /[\s"=]/.
func jsSpace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0xa0, 0x1680, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000, 0xfeff:
		return true
	}
	return r >= 0x2000 && r <= 0x200a
}

// jsQuote is JavaScript's JSON.stringify for a string, which escapes less
// than json.Marshal does (no \u003c, no \u2028).
func jsQuote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 {
				fmt.Fprintf(&b, `\u%04x`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

// checkMeta refuses a meta key that is not an identifier (letters, digits,
// underscore): the Claude Code client silently DROPS anything else, so a bad
// key is refused where the caller can see it.
func checkMeta(meta map[string]string) error {
	for k := range meta {
		for _, r := range k {
			ok := r == '_' || (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
			if !ok {
				return fmt.Errorf("meta key %q is not an identifier — the client would drop it silently", k)
			}
		}
	}
	return nil
}

// StartWatch is the start every tool's `<tool> watch` makes. In a bridge child
// (harness.Capture.Child: pi's claude-bridge runs this Claude Code session as
// part of a pi session, whose own channel carrier delivers the events) it
// prints {"stop": reason} and returns run false: the caller exits 0, and the
// mod logs the reason and never restarts watch for this session. Otherwise it
// returns the Lines writer for tool's events.
func StartWatch(w io.Writer, tool string) (l *Lines, run bool, err error) {
	if harness.FromEnv().Child {
		b, err := json.Marshal(struct {
			Stop string `json:"stop"`
		}{"bridge child: the hosting pi session's channel delivers " + tool + " events"})
		if err != nil {
			return nil, false, err
		}
		_, err = w.Write(append(b, '\n'))
		return nil, false, err
	}
	return NewLinesFor(w, tool), true, nil
}
