package mermaidkit

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const fakeMermaid = "/*MERMAID*/"

var fakeBundle = Bundle{JS: []byte(fakeMermaid), Global: "toolMermaid"}

func render(t *testing.T, md string, th Theme) string {
	t.Helper()
	out, err := Render([]byte(md), th, fakeBundle, "")
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func TestRenderIsSelfContained(t *testing.T) {
	md := "# Title\n\nSee https://example.com and [this](https://example.com/x).\n\n## Data\n\n| a | b |\n|---|---|\n| 1 | 2 |\n\n```mermaid\ngraph TD\n  A-->B\n```\n"
	out := render(t, md, BaseTheme())
	for _, bad := range []string{`src="http`, `<link rel="stylesheet"`, `@import`, `url(http`} {
		if strings.Contains(out, bad) {
			t.Errorf("output contains %q", bad)
		}
	}
	if !strings.Contains(out, "<table>") {
		t.Error("table did not render")
	}
}

func TestRenderBuildsSidebar(t *testing.T) {
	out := render(t, "# T\n\n## One\n\n### Two\n\n#### Three\n", BaseTheme())
	_, nav, ok := strings.Cut(out, "<nav")
	nav, _, closed := strings.Cut(nav, "</nav>")
	if !ok || !closed {
		t.Fatalf("no sidebar: %s", out)
	}
	for _, want := range []string{`href="#one"`, `href="#two"`} {
		if !strings.Contains(nav, want) {
			t.Errorf("sidebar lacks %s: %s", want, nav)
		}
	}
	if strings.Contains(nav, "three") || strings.Contains(nav, "Three") {
		t.Errorf("sidebar lists an h4: %s", nav)
	}
}

func TestRenderMermaidBlock(t *testing.T) {
	out := render(t, "# T\n\n```mermaid\ngraph TD\n  A-->B\n```\n", BaseTheme())
	if !strings.Contains(out, `<pre class="mermaid"`) {
		t.Error("no mermaid block")
	}
	if !strings.Contains(out, "A--&gt;B") {
		t.Error("diagram source missing")
	}
	if n := strings.Count(out, fakeMermaid); n != 1 {
		t.Errorf("mermaid.js appears %d times, want 1", n)
	}
}

func TestRenderNoMermaidNoScript(t *testing.T) {
	out := render(t, "# T\n\nplain\n\n```go\nfunc x() {}\n```\n", BaseTheme())
	if strings.Contains(out, fakeMermaid) {
		t.Error("mermaid.js included for a document without a diagram")
	}
}

// The page draws its diagrams with the global the bundle was built with,
// and an inlined bundle cannot close its own <script>.
// With no global the page would show every diagram's source and draw none.
func TestRenderRefusesABundleWithNoGlobal(t *testing.T) {
	for _, md := range []string{"```mermaid\ngraph TD\n```\n", "# no diagram\n"} {
		out, err := Render([]byte(md), BaseTheme(), Bundle{JS: fakeBundle.JS}, "")
		if err == nil || out != nil || !strings.Contains(err.Error(), "Bundle.Global") {
			t.Fatalf("%q: got %v; want an error naming Bundle.Global", md, err)
		}
	}
}

func TestRenderDrawsWithTheBundlesGlobal(t *testing.T) {
	b := Bundle{JS: []byte("var x = '</script>';"), Global: "toolMermaid"}
	out, err := Render([]byte("```mermaid\ngraph TD\n```\n"), BaseTheme(), b, "")
	if err != nil {
		t.Fatal(err)
	}
	page := string(out)
	if !strings.Contains(page, `window["toolMermaid"]`) {
		t.Errorf("page does not read window.toolMermaid:\n%s", page)
	}
	if strings.Contains(page, "galleyMermaid") {
		t.Error("page names galley's global")
	}
	if !strings.Contains(page, `var x = '<\/script>';`) {
		t.Errorf("inlined bundle not script-safe:\n%s", page)
	}
}

// Render renders md as given: removing what a page must not show is the
// tool's (galley clears review instructions first).
func TestRenderKeepsTheMarkdownItIsGiven(t *testing.T) {
	out := render(t, "Keep {==this==}{>>change it<<} text\n", BaseTheme())
	if !strings.Contains(out, "change it") {
		t.Error("Render removed text it was given")
	}
}

func TestRenderUsesTheme(t *testing.T) {
	th := BaseTheme()
	th.Light.Accent = "#123456"
	th.Label = "Acme"
	th.ExtraCSS = ".x{}"
	out := render(t, "# T\n", th)
	for _, want := range []string{"#123456", "Acme", ".x{}"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q", want)
		}
	}
}

func TestRenderBaseHasNoHeader(t *testing.T) {
	out := render(t, "# T\n\n## One\n", BaseTheme())
	if strings.Contains(out, "<header") {
		t.Errorf("base theme rendered a header: %s", out)
	}
}

func TestRenderLabelOnlyHeader(t *testing.T) {
	th := BaseTheme()
	th.Label = "Acme"
	out := render(t, "# T\n", th)
	head, _, _ := strings.Cut(out, "</header>")
	_, head, ok := strings.Cut(head, "<header")
	if !ok {
		t.Fatalf("no header: %s", out)
	}
	if !strings.Contains(head, "Acme") {
		t.Errorf("header lacks the label: %s", head)
	}
	if strings.Contains(head, "<img") || strings.Contains(head, "g-logo") {
		t.Errorf("header has a logo: %s", head)
	}
}

func TestRenderLogoHeader(t *testing.T) {
	th := BaseTheme()
	th.LogoLight = []byte("<svg>light</svg>")
	th.LogoDark = []byte("<svg>dark</svg>")
	out := render(t, "# T\n", th)
	for _, want := range []string{inPage(th.LogoLight), inPage(th.LogoDark)} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks logo %s", want)
		}
	}
	if strings.Contains(out, `class="g-label"`) {
		t.Error("an empty label rendered")
	}
}

func TestRenderMissingDarkLogoUsesLight(t *testing.T) {
	th := BaseTheme()
	th.LogoLight = []byte("<svg>light</svg>")
	out := render(t, "# T\n", th)
	light := inPage(th.LogoLight)
	if !strings.Contains(out, `srcset="`+light+`"`) {
		t.Errorf("dark scheme does not fall back to the light logo: %s", out)
	}
}

// inPage is a logo's data: URL as the page writes it, with "+" escaped.
func inPage(svg []byte) string {
	return strings.ReplaceAll(string(svgURL(svg)), "+", "&#43;")
}

// pngBytes is any bytes; the renderer embeds a file as it is.
const pngBytes = "\x89PNG\r\n\x1a\nfake"

func renderIn(t *testing.T, dir, md string) string {
	t.Helper()
	out, err := Render([]byte(md), BaseTheme(), fakeBundle, dir)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func TestRenderEmbedsLocalImages(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "doc")
	writeFile(t, filepath.Join(dir, "a.png"), pngBytes)
	writeFile(t, filepath.Join(dir, "figs", "b.png"), pngBytes)
	writeFile(t, filepath.Join(root, "outside.png"), pngBytes)
	if err := os.Symlink(filepath.Join(root, "outside.png"), filepath.Join(dir, "link.png")); err != nil {
		t.Fatal(err)
	}
	data := `src="data:image/png;base64,` + base64.StdEncoding.EncodeToString([]byte(pngBytes)) + `"`
	for _, tc := range []struct {
		name, src string
		embed     bool
	}{
		{"beside the doc", "a.png", true},
		{"in a subfolder", "figs/b.png", true},
		{"outside the folder", "../outside.png", false},
		{"a symlink out of the folder", "link.png", false},
		{"remote", "https://example.com/c.png", false},
		{"missing", "nope.png", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := renderIn(t, dir, "# T\n\n![x]("+tc.src+")\n")
			if tc.embed && !strings.Contains(out, data) {
				t.Fatalf("%s not embedded: %s", tc.src, out)
			}
			if !tc.embed && !strings.Contains(out, `src="`+tc.src+`"`) {
				t.Fatalf("%s did not keep its src: %s", tc.src, out)
			}
		})
	}
}
