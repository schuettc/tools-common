package mermaidkit

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeTheme(t *testing.T, themeJSON string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "acme")
	writeFile(t, filepath.Join(dir, "theme.json"), themeJSON)
	writeFile(t, filepath.Join(dir, "logo-light.svg"), "<svg>light</svg>")
	writeFile(t, filepath.Join(dir, "logo-dark.svg"), "<svg>dark</svg>")
	return dir
}

func TestReadThemeFillsMissingFromBase(t *testing.T) {
	got, err := ReadTheme("acme", writeTheme(t, `{"label":"Acme","light":{"accent":"#123456"}}`))
	if err != nil {
		t.Fatal(err)
	}
	base := BaseTheme()
	if got.Name != "acme" || got.Label != "Acme" {
		t.Errorf("Name, Label = %q, %q", got.Name, got.Label)
	}
	if got.Light.Accent != "#123456" {
		t.Errorf("Light.Accent = %q", got.Light.Accent)
	}
	if got.Light.Bg != base.Light.Bg {
		t.Errorf("Light.Bg = %q, want base %q", got.Light.Bg, base.Light.Bg)
	}
	if got.Dark != base.Dark {
		t.Errorf("Dark = %+v, want base %+v", got.Dark, base.Dark)
	}
	if string(got.LogoLight) != "<svg>light</svg>" || string(got.LogoDark) != "<svg>dark</svg>" {
		t.Errorf("logos = %q %q", got.LogoLight, got.LogoDark)
	}
}

func TestReadThemeWithoutThemeJSONErrors(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nope")
	_, err := ReadTheme("nope", dir)
	if err == nil || !strings.Contains(err.Error(), `theme "nope"`) || !strings.Contains(err.Error(), dir) {
		t.Fatalf("err = %v, want it to name the theme and %s", err, dir)
	}
}

func TestReadThemeMalformedJSONErrors(t *testing.T) {
	_, err := ReadTheme("bad", writeTheme(t, `{"label":`))
	if err == nil || !strings.Contains(err.Error(), "theme.json") {
		t.Fatalf("err = %v, want one naming theme.json", err)
	}
}

func TestReadThemeReadsFontsAndExtraCSS(t *testing.T) {
	dir := writeTheme(t, `{"fonts":[{"family":"Aeonik","file":"Aeonik-Regular.woff2","weight":"400"}]}`)
	font := []byte{0x77, 0x4f, 0x46, 0x32, 0x00, 0x01}
	if err := os.WriteFile(filepath.Join(dir, "Aeonik-Regular.woff2"), font, 0o644); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "extra.css"), ".acme{color:red}")
	got, err := ReadTheme("acme", dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Fonts) != 1 || !bytes.Equal(got.Fonts[0].Data, font) || got.Fonts[0].Family != "Aeonik" {
		t.Fatalf("Fonts = %+v", got.Fonts)
	}
	if got.ExtraCSS != ".acme{color:red}" {
		t.Fatalf("ExtraCSS = %q", got.ExtraCSS)
	}
	out, err := Render([]byte("# T\n"), got, fakeBundle, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `font-family:"Aeonik"`) || !strings.Contains(string(out), "font/woff2") {
		t.Errorf("page does not embed the font")
	}
}

func TestReadThemeMissingFontFileErrors(t *testing.T) {
	_, err := ReadTheme("acme", writeTheme(t, `{"fonts":[{"family":"A","file":"gone.woff2"}]}`))
	if err == nil || !strings.Contains(err.Error(), "gone.woff2") {
		t.Fatalf("err = %v, want one naming the font file", err)
	}
}

// A theme that leaves out a logo file has no logo for that scheme.
func TestReadThemeMissingLogoIsEmpty(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "plain")
	writeFile(t, filepath.Join(dir, "theme.json"), `{}`)
	got, err := ReadTheme("plain", dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.LogoLight) != 0 || len(got.LogoDark) != 0 || got.Label != "" {
		t.Fatalf("got logos %q %q label %q, want none", got.LogoLight, got.LogoDark, got.Label)
	}
}
