package channelmcp

import (
	"encoding/json"
	"fmt"
	"io"
	"sync"
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

// Lines writes each event as one JSON line, {"content":...,"meta":{...}}, to
// w. Notify is safe from several goroutines: each line is written whole,
// under one mutex, so lines never interleave.
type Lines struct {
	mu sync.Mutex
	w  io.Writer
}

// NewLines returns a Pusher that prints to w.
func NewLines(w io.Writer) *Lines { return &Lines{w: w} }

// Notify refuses the same meta keys Server.Notify refuses, so an event that
// works on one output works on the other. A nil meta prints as {}.
func (l *Lines) Notify(content string, meta map[string]string) error {
	if err := checkMeta(meta); err != nil {
		return err
	}
	if meta == nil {
		meta = map[string]string{}
	}
	b, err := json.Marshal(struct {
		Content string            `json:"content"`
		Meta    map[string]string `json:"meta"`
	}{content, meta})
	if err != nil {
		return err
	}
	b = append(b, '\n')
	l.mu.Lock()
	defer l.mu.Unlock()
	_, err = l.w.Write(b)
	return err
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
