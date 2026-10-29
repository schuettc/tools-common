// Package sessiontheme resolves persistent terminal assignments into shared colors.
package sessiontheme

import (
	"fmt"
	"math"
	"strconv"
)

// Hue is the window-owned color family.
type Hue string

// hueCatalog is the single authored source of allocation order and base colors.
var hueCatalog = [...]struct {
	hue                            Hue
	background, foreground, accent string
}{
	{"blue", "#14283f", "#dfebfa", "#94c4ff"},
	{"teal", "#12332f", "#dcefe9", "#8bdfcb"},
	{"green", "#22341e", "#e4efdc", "#b9de94"},
	{"olive", "#34351b", "#eeefd9", "#dce08b"},
	{"amber", "#3d2c18", "#f5ead5", "#efc276"},
	{"rust", "#3e231d", "#f5e3da", "#efb08e"},
	{"rose", "#3b2030", "#f3e1ed", "#edacd3"},
	{"purple", "#2c2042", "#eee2fb", "#d1b0ff"},
}

// Values returns the canonical allocation order in a fresh slice, using the
// family's enum interface so wiregen can retain the vocabulary in consumers.
func (Hue) Values() []string {
	values := make([]string, len(hueCatalog))
	for i, base := range hueCatalog {
		values[i] = string(base.hue)
	}
	return values
}

// Shade is a session-owned slot within a window's hue.
type Shade string

// Values returns the canonical shade allocation order in a fresh slice.
func (Shade) Values() []string { return []string{"midnight", "deep", "muted", "bright"} }

// Assignment retains the window hue and session shade independently.
type Assignment struct {
	Hue   Hue   `json:"hue"`
	Shade Shade `json:"shade"`
}

// Palette supplies terminal, tmux, pi, and header colors as six-digit sRGB hex.
type Palette struct {
	Background         string     `json:"background"`
	Foreground         string     `json:"foreground"`
	Panel              string     `json:"panel"`
	SelectedBackground string     `json:"selected_background"`
	Muted              string     `json:"muted"`
	Border             string     `json:"border"`
	Accent             string     `json:"accent"`
	AccentText         string     `json:"accent_text"`
	Success            string     `json:"success"`
	Warning            string     `json:"warning"`
	Error              string     `json:"error"`
	Cursor             string     `json:"cursor"`
	CursorText         string     `json:"cursor_text"`
	ANSI               [16]string `json:"ansi"`
}

// Header is the scoped treatment of a diazo plan header.
type Header struct {
	Background string `json:"background"`
	Foreground string `json:"foreground"`
	Accent     string `json:"accent"`
}

// HeaderFor derives a header without changing the page body theme.
func HeaderFor(p Palette) Header { return Header{p.Background, p.Foreground, p.Accent} }

// Resolve returns a deterministic palette, rejecting unknown vocabulary.
func Resolve(a Assignment) (Palette, error) {
	var base [3]string
	found := false
	for _, entry := range hueCatalog {
		if entry.hue == a.Hue {
			base = [3]string{entry.background, entry.foreground, entry.accent}
			found = true
			break
		}
	}
	if !found {
		return Palette{}, fmt.Errorf("unknown theme hue %q", a.Hue)
	}
	bg := base[0]
	switch a.Shade {
	case "midnight":
		bg = mix(bg, "#000000", .20)
	case "deep":
	case "muted":
		bg = mix(bg, "#454b55", .30)
	case "bright":
		bg = mix(bg, base[2], .14)
	default:
		return Palette{}, fmt.Errorf("unknown theme shade %q", a.Shade)
	}
	p := Palette{Background: bg, Panel: mix(bg, "#000000", .08), SelectedBackground: mix(bg, "#000000", .22)}
	// All normal foreground roles remain legible even on the lightest UI surface.
	surfaces := []string{p.Background, p.Panel, p.SelectedBackground}
	p.Foreground = readable(base[1], surfaces)
	p.Accent = readable(base[2], surfaces)
	p.Muted = readable(mix(p.Foreground, bg, .25), surfaces)
	p.Border = p.Muted
	p.Success = readable("#b4e5ac", surfaces)
	p.Warning = readable("#f3d18a", surfaces)
	p.Error = readable("#ffa9a9", surfaces)
	p.AccentText = bg
	p.Cursor = p.Accent
	p.CursorText = p.AccentText
	// ANSI positions retain terminal semantics; bright variants lighten, not rotate.
	p.ANSI = [16]string{mix(bg, "#000000", .35), p.Error, p.Success, p.Warning,
		readable("#94c4ff", surfaces), readable("#d1b0ff", surfaces), readable("#8bdfcb", surfaces), p.Foreground,
		p.Muted, mix(p.Error, "#ffffff", .15), mix(p.Success, "#ffffff", .15), mix(p.Warning, "#ffffff", .15),
		mix(readable("#94c4ff", surfaces), "#ffffff", .15), mix(readable("#d1b0ff", surfaces), "#ffffff", .15), mix(readable("#8bdfcb", surfaces), "#ffffff", .15), "#ffffff"}
	return p, nil
}

func components(s string) [3]float64 {
	n, _ := strconv.ParseUint(s[1:], 16, 24)
	return [3]float64{float64(n >> 16), float64((n >> 8) & 255), float64(n & 255)}
}

// mix interpolates encoded sRGB components with nearest-integer rounding.
func mix(a, b string, amount float64) string {
	x, y := components(a), components(b)
	return fmt.Sprintf("#%02x%02x%02x", int(math.Round(x[0]*(1-amount)+y[0]*amount)), int(math.Round(x[1]*(1-amount)+y[1]*amount)), int(math.Round(x[2]*(1-amount)+y[2]*amount)))
}

func luminance(s string) float64 {
	rgb := components(s)
	for i, c := range rgb {
		c /= 255
		if c <= .04045 {
			rgb[i] = c / 12.92
		} else {
			rgb[i] = math.Pow((c+.055)/1.055, 2.4)
		}
	}
	return .2126*rgb[0] + .7152*rgb[1] + .0722*rgb[2]
}

func readable(color string, surfaces []string) string {
	for step := 0; step <= 100; step++ {
		candidate := mix(color, "#ffffff", float64(step)/100)
		ok := true
		for _, bg := range surfaces {
			x, y := luminance(candidate), luminance(bg)
			if x < y {
				x, y = y, x
			}
			if (x+.05)/(y+.05) < 4.5 {
				ok = false
				break
			}
		}
		if ok {
			return candidate
		}
	}
	return "#ffffff"
}
