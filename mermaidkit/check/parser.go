// Package check checks a mermaid diagram's syntax on the server: Parser runs
// mermaid's own parser in goja, so nothing at runtime needs node, and Check
// returns mermaid's reason for a diagram it will not parse.
//
// It is apart from mermaidkit's renderer so that a tool that only checks
// diagrams (diazo) never links goldmark, and one that only renders (galley)
// never links goja.
package check

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/dop251/goja"
	"github.com/subaud-tools/tools-common/mermaidkit/internal/bundle"
)

// Bundle is a tool's mermaid bundle: the IIFE esbuild built from the shared
// entry, localweb/page/build/mermaid-entry.ts, with --format=iife (JS), and
// the --global-name it was built with (Global), the window property whose
// default export is mermaid. It is mermaidkit.Bundle: one type, so a tool
// that both renders and checks builds it once.
type Bundle = bundle.Bundle

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

// NewParser is a Parser over b. It refuses a bundle with no Global, the name
// that must match the tool's esbuild --global-name.
func NewParser(b Bundle) (*Parser, error) {
	if err := bundle.Validate(b); err != nil {
		return nil, err
	}
	return &Parser{bundle: b}, nil
}

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
