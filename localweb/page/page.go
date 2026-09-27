// Package page is the family's galley-style local page kit: one stylesheet of
// tokens and components plus vanilla ES modules (theme, keyboard, API, live
// updates, DOM builders), embedded and served under /_kit/ beside a tool's own
// page. Pair it with localweb:
//
//	localweb.Start(ctx, localweb.Config{Tool: "cull", Assets: page.With(webFS), API: api})
//
// See tools-ops docs/superpowers/specs/2026-09-26-localweb-page-kit-design.md.
package page

import (
	"embed"
	"io/fs"
	"sort"
	"strings"
)

// Prefix is where the kit's assets are served: a page imports "/_kit/kit.js".
const Prefix = "_kit"

//go:embed assets
var assets embed.FS

// FS is the kit's embedded assets, rooted at the asset directory.
func FS() fs.FS {
	sub, err := fs.Sub(assets, "assets")
	if err != nil {
		panic(err) // the embed directive guarantees the directory
	}
	return sub
}

// With returns an fs.FS that serves the tool's own assets at / and the kit's
// under /_kit/. The kit wins on the prefix; a tool file at _kit/... is
// unreachable. tool may be nil for a page made only of kit files.
func With(tool fs.FS) fs.FS { return overlay{tool: tool, kit: FS()} }

type overlay struct{ tool, kit fs.FS }

// kitName maps a name under the prefix to the kit's own name.
func kitName(name string) (string, bool) {
	if name == Prefix {
		return ".", true
	}
	if rest, ok := strings.CutPrefix(name, Prefix+"/"); ok {
		return rest, true
	}
	return "", false
}

func (o overlay) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	if k, ok := kitName(name); ok {
		return o.kit.Open(k)
	}
	if o.tool == nil {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	return o.tool.Open(name)
}

func (o overlay) ReadDir(name string) ([]fs.DirEntry, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrInvalid}
	}
	if k, ok := kitName(name); ok {
		return fs.ReadDir(o.kit, k)
	}
	var out []fs.DirEntry
	if o.tool != nil {
		es, err := fs.ReadDir(o.tool, name)
		if err != nil && name != "." {
			return nil, err
		}
		for _, e := range es {
			if name == "." && e.Name() == Prefix {
				continue
			}
			out = append(out, e)
		}
	} else if name != "." {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrNotExist}
	}
	if name == "." {
		info, err := fs.Stat(o.kit, ".")
		if err != nil {
			return nil, err
		}
		out = append(out, prefixEntry{fs.FileInfoToDirEntry(info)})
		sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	}
	return out, nil
}

// prefixEntry is the kit's root directory entry, renamed to the prefix.
type prefixEntry struct{ fs.DirEntry }

func (prefixEntry) Name() string { return Prefix }
