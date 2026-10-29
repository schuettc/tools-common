package sessiontheme

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"testing"
)

var hexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

func TestResolveCollection(t *testing.T) {
	hues := Hue("").Values()
	shades := Shade("").Values()
	if fmt.Sprint(hues) != "[blue teal green olive amber rust rose purple]" || fmt.Sprint(shades) != "[midnight deep muted bright]" {
		t.Fatal("incomplete assignment vocabulary", hues, shades)
	}
	for _, h := range hues {
		for _, s := range shades {
			t.Run(string(h)+"-"+string(s), func(t *testing.T) {
				p, err := Resolve(Assignment{Hue(h), Shade(s)})
				if err != nil {
					t.Fatal(err)
				}
				again, err := Resolve(Assignment{Hue(h), Shade(s)})
				if err != nil || p != again {
					t.Fatal("resolution is not deterministic")
				}
				data, err := json.Marshal(p)
				if err != nil {
					t.Fatal(err)
				}
				var fields map[string]json.RawMessage
				if err := json.Unmarshal(data, &fields); err != nil {
					t.Fatal(err)
				}
				for key, raw := range fields {
					var colors []string
					if key == "ansi" {
						if err := json.Unmarshal(raw, &colors); err != nil {
							t.Fatal(err)
						}
						if len(colors) != 16 {
							t.Fatal("ANSI requires 16 entries")
						}
					} else {
						var c string
						if err := json.Unmarshal(raw, &c); err != nil {
							t.Fatal(err)
						}
						colors = []string{c}
					}
					for _, c := range colors {
						if !hexColor.MatchString(c) {
							t.Fatalf("%s: invalid color %q", key, c)
						}
					}
				}
				for _, bg := range []string{p.Background, p.Panel, p.SelectedBackground} {
					for _, fg := range []string{p.Foreground, p.Accent, p.Muted, p.Success, p.Warning, p.Error} {
						if ratio(fg, bg) < 4.5 {
							t.Errorf("%s on %s: contrast %.2f", fg, bg, ratio(fg, bg))
						}
					}
				}
				if ratio(p.AccentText, p.Accent) < 4.5 || ratio(p.CursorText, p.Cursor) < 4.5 {
					t.Fatal("unreadable accent or cursor text")
				}
				if p.Success != "#b4e5ac" || p.Warning != "#f3d18a" || p.Error != "#ffa9a9" || p.ANSI[1] != p.Error || p.ANSI[2] != p.Success || p.ANSI[3] != p.Warning {
					t.Fatal("semantic roles lost")
				}
				if HeaderFor(p) != (Header{p.Background, p.Foreground, p.Accent}) {
					t.Fatal("header disagrees with terminal")
				}
			})
		}
	}
	for _, a := range []Assignment{{"", "deep"}, {"cyan", "deep"}, {"blue", ""}, {"blue", "ice"}} {
		if _, err := Resolve(a); err == nil {
			t.Errorf("accepted invalid assignment %+v", a)
		}
	}
	// Hand-rounded sRGB samples pin the prescribed transforms, not merely contrast.
	for _, tc := range []struct {
		h              Hue
		s              Shade
		bg, fg, accent string
	}{
		{"blue", "midnight", "#102032", "#dfebfa", "#94c4ff"},
		{"blue", "muted", "#233346", "#dfebfa", "#94c4ff"},
		{"blue", "bright", "#263e5a", "#dfebfa", "#94c4ff"},
		{"blue", "deep", "#14283f", "#dfebfa", "#94c4ff"},
		{"teal", "deep", "#12332f", "#dcefe9", "#8bdfcb"},
		{"green", "deep", "#22341e", "#e4efdc", "#b9de94"},
		{"olive", "deep", "#34351b", "#eeefd9", "#dce08b"},
		{"amber", "deep", "#3d2c18", "#f5ead5", "#efc276"},
		{"rust", "deep", "#3e231d", "#f5e3da", "#efb08e"},
		{"rose", "deep", "#3b2030", "#f3e1ed", "#edacd3"},
		{"purple", "deep", "#2c2042", "#eee2fb", "#d1b0ff"},
	} {
		p, err := Resolve(Assignment{tc.h, tc.s})
		if err != nil {
			t.Fatal(err)
		}
		if p.Background != tc.bg || p.Foreground != tc.fg || p.Accent != tc.accent {
			t.Errorf("%s/%s: got %s %s %s", tc.h, tc.s, p.Background, p.Foreground, p.Accent)
		}
	}
}

func ratio(a, b string) float64 {
	lum := func(s string) float64 {
		var r, g, b uint64
		if _, err := fmt.Sscanf(s, "#%02x%02x%02x", &r, &g, &b); err != nil {
			panic(err)
		}
		linear := func(v uint64) float64 {
			x := float64(v) / 255
			if x <= 0.04045 {
				return x / 12.92
			}
			return math.Pow((x+0.055)/1.055, 2.4)
		}
		return .2126*linear(r) + .7152*linear(g) + .0722*linear(b)
	}
	x, y := lum(a), lum(b)
	if x < y {
		x, y = y, x
	}
	return (x + .05) / (y + .05)
}
