package mermaidkit

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Theme is the look of a rendered page. Every theme shares one layout; a
// theme changes only colours, fonts, logo, label and extra CSS.
type Theme struct {
	Name, Label         string
	Light, Dark         Palette
	Fonts               []Font
	LogoLight, LogoDark []byte
	ExtraCSS            string
}

// Palette is one colour scheme's five colours.
type Palette struct {
	Bg     string `json:"bg"`
	Fg     string `json:"fg"`
	Muted  string `json:"muted"`
	Line   string `json:"line"`
	Accent string `json:"accent"`
}

// Font is one font file to embed. Data is the file's bytes, read at load.
type Font struct {
	Family string `json:"family"`
	File   string `json:"file"`
	Data   []byte `json:"-"`
	Weight string `json:"weight"`
	Style  string `json:"style"`
}

//go:embed base
var baseFS embed.FS

// themeFile is theme.json as written; every field may be left out.
type themeFile struct {
	Label string  `json:"label"`
	Light Palette `json:"light"`
	Dark  Palette `json:"dark"`
	Fonts []Font  `json:"fonts"`
}

// BaseTheme is the family's own look, used when a document names no theme
// and for every colour a theme leaves out. It has no logo and no label, so
// its page has no header.
func BaseTheme() Theme {
	raw, err := baseFS.ReadFile("base/theme.json")
	if err != nil {
		panic(err)
	}
	var f themeFile
	if err := json.Unmarshal(raw, &f); err != nil {
		panic(err)
	}
	return Theme{Name: "base", Label: f.Label, Light: f.Light, Dark: f.Dark}
}

// ReadTheme reads the theme folder dir as the theme called name: its
// theme.json (every field optional, colours left out filled from the base
// theme), the font files it names, logo-light.svg, logo-dark.svg and
// extra.css. Where themes live, and which one a document uses, is the tool's.
func ReadTheme(name, dir string) (Theme, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "theme.json"))
	if errors.Is(err, fs.ErrNotExist) {
		return Theme{}, fmt.Errorf("theme %q: no theme.json in %s", name, dir)
	}
	if err != nil {
		return Theme{}, fmt.Errorf("theme %q: %w", name, err)
	}
	var f themeFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return Theme{}, fmt.Errorf("theme %q: %s: %w", name, filepath.Join(dir, "theme.json"), err)
	}
	t := BaseTheme()
	t.Name = name
	if f.Label != "" {
		t.Label = f.Label
	}
	t.Light = fill(f.Light, t.Light)
	t.Dark = fill(f.Dark, t.Dark)
	for _, font := range f.Fonts {
		data, err := os.ReadFile(filepath.Join(dir, font.File))
		if err != nil {
			return Theme{}, fmt.Errorf("theme %q: font file %s: %w", name, font.File, err)
		}
		font.Data = data
		t.Fonts = append(t.Fonts, font)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "logo-light.svg")); err == nil {
		t.LogoLight = b
	}
	if b, err := os.ReadFile(filepath.Join(dir, "logo-dark.svg")); err == nil {
		t.LogoDark = b
	}
	if b, err := os.ReadFile(filepath.Join(dir, "extra.css")); err == nil {
		t.ExtraCSS = string(b)
	}
	return t, nil
}

// fill takes each colour p leaves empty from base.
func fill(p, base Palette) Palette {
	pick := func(v, d string) string {
		if v == "" {
			return d
		}
		return v
	}
	return Palette{
		Bg:     pick(p.Bg, base.Bg),
		Fg:     pick(p.Fg, base.Fg),
		Muted:  pick(p.Muted, base.Muted),
		Line:   pick(p.Line, base.Line),
		Accent: pick(p.Accent, base.Accent),
	}
}
