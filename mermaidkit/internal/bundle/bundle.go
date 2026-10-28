// Package bundle holds the one type mermaidkit's renderer and its parser
// (mermaidkit/check) share, apart from both, so that importing either one
// links neither the other nor its dependency (goldmark, goja).
package bundle

import "errors"

// Bundle is a tool's mermaid bundle: the IIFE esbuild built from
// mermaid-entry.ts, and the --global-name it was built with, the window
// property whose default export is mermaid.
type Bundle struct {
	JS     []byte
	Global string
}

// Validate refuses a Bundle with no Global: the page and the parser look the
// bundle up by it, and with none no diagram is ever drawn or checked.
func Validate(b Bundle) error {
	if b.Global == "" {
		return errors.New("mermaidkit: Bundle.Global is empty: set it to the --global-name the tool's esbuild built mermaid.js with")
	}
	return nil
}
