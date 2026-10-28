// Package mermaidkit is what the family's page tools do with mermaid: check a
// diagram's syntax on the server (Parser, mermaid's own parser run in goja, so
// nothing at runtime needs node), and render a markdown document as one
// themed, self-contained HTML page whose diagrams the browser draws (Render).
//
// Each tool builds its own mermaid bundle from the shared entry,
// localweb/page/build/mermaid-entry.ts, with esbuild's --format=iife and a
// --global-name of its own, and hands the bytes and that name in as a Bundle.
//
// It is its own module (tagged mermaidkit/vX.Y.Z) so that the root
// tools-common module stays free of goja and goldmark.
package mermaidkit

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/dop251/goja"
)

// Bundle is a tool's mermaid bundle: the IIFE esbuild built from
// mermaid-entry.ts, and the --global-name it was built with, the window
// property whose default export is mermaid.
type Bundle struct {
	JS     []byte
	Global string
}

// globals are what the bundle's IIFE reaches for outside a browser. Its
// timers run at once, so parse's promise settles before RunString returns.
const globals = `var window = globalThis; var self = globalThis;
var console = {log() {}, warn() {}, error() {}, info() {}, debug() {}, trace() {}};
var setTimeout = function (f) { f(); return 0; }; var clearTimeout = function () {};`

// Parser parses diagrams with the mermaid bundle it was given. The bundle
// is loaded into one runtime on the first Check and reused after that; a
// goja runtime is not safe for concurrent use, so calls take turns.
type Parser struct {
	bundle Bundle

	mu    sync.Mutex
	vm    *goja.Runtime
	api   goja.Value // <Global>.default: parse's this
	parse goja.Callable
	err   error // the bundle failed to load: every Check returns it
}

// NewParser is a Parser over b.
func NewParser(b Bundle) *Parser { return &Parser{bundle: b} }

// Check is nil when mermaid parses text, or mermaid's reason (its first two
// lines) when it does not.
func (p *Parser) Check(text string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.vm == nil && p.err == nil {
		p.err = p.load()
	}
	if p.err != nil {
		return p.err
	}
	v, err := p.parse(p.api, p.vm.ToValue(text))
	if err != nil {
		return reason(err.Error())
	}
	pr, ok := v.Export().(*goja.Promise)
	if !ok {
		return nil
	}
	switch pr.State() {
	case goja.PromiseStateRejected:
		return reason(message(pr.Result()))
	case goja.PromiseStatePending:
		return errors.New("mermaid did not finish parsing the diagram")
	}
	return nil
}

func (p *Parser) load() error {
	vm := goja.New()
	if _, err := vm.RunString(globals); err != nil {
		return fmt.Errorf("mermaid: %w", err)
	}
	if _, err := vm.RunScript("mermaid.js", string(p.bundle.JS)); err != nil {
		return fmt.Errorf("mermaid: loading the bundle: %w", err)
	}
	g := p.bundle.Global
	var api goja.Value
	if m := vm.Get(g); m != nil && !goja.IsUndefined(m) {
		api = m.ToObject(vm).Get("default")
	}
	if api == nil || goja.IsUndefined(api) {
		return fmt.Errorf("mermaid: the bundle defines no %s.default", g)
	}
	parse, ok := goja.AssertFunction(api.ToObject(vm).Get("parse"))
	if !ok {
		return fmt.Errorf("mermaid: the bundle has no %s.default.parse", g)
	}
	p.vm, p.api, p.parse = vm, api, parse
	return nil
}

// message is a rejection's text: an Error's message, or the value itself.
func message(v goja.Value) string {
	if o, ok := v.(*goja.Object); ok {
		if m := o.Get("message"); m != nil && !goja.IsUndefined(m) {
			return m.String()
		}
	}
	return v.String()
}

// reason is mermaid's first two lines.
func reason(s string) error {
	lines := strings.SplitN(strings.TrimSpace(s), "\n", 3)
	return errors.New(strings.Join(lines[:min(2, len(lines))], "\n"))
}
