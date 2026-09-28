package page

import (
	"io/fs"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

var kitDecl = regexp.MustCompile(`(?m)(--kit-[a-z0-9-]+)\s*:`)

// wantTokens is spec §3.1's table: the only --kit-* properties there are.
var wantTokens = []string{
	"bg", "panel", "card", "fg", "muted", "line",
	"signal", "signal-soft", "signal-ink", "agent", "agent-soft",
	"wait", "danger", "ok", "lift", "mono", "sans",
}

// declarations maps "<source>/<file>" to the --kit-* names it declares.
func declarations(t *testing.T) map[string][]string {
	t.Helper()
	out := map[string][]string{}
	scan := func(label string, fsys fs.FS) {
		_ = fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			b, err := fs.ReadFile(fsys, p)
			if err != nil {
				t.Fatal(err)
			}
			for _, m := range kitDecl.FindAllStringSubmatch(string(b), -1) {
				out[label+"/"+p] = append(out[label+"/"+p], m[1])
			}
			return nil
		})
	}
	scan("assets", FS())
	for _, dir := range []string{"demo", "testdata"} {
		if _, err := os.Stat(dir); err == nil {
			scan(dir, os.DirFS(dir))
		}
	}
	return out
}

func TestTokensOneSource(t *testing.T) {
	decls := declarations(t)
	for file, names := range decls {
		if file != "assets/kit.css" {
			t.Errorf("%s declares %v; only kit.css may declare --kit-* tokens", file, names)
		}
	}
	seen := map[string]int{}
	for _, n := range decls["assets/kit.css"] {
		seen[n]++
	}
	for n, c := range seen {
		if c != 1 {
			t.Errorf("%s declared %d times", n, c)
		}
	}
	var got, want []string
	for n := range seen {
		got = append(got, n)
	}
	for _, n := range wantTokens {
		want = append(want, "--kit-"+n)
	}
	sort.Strings(got)
	sort.Strings(want)
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("tokens:\n got %v\nwant %v", got, want)
	}
}

func TestKitCSSHeader(t *testing.T) {
	b, _ := fs.ReadFile(FS(), "kit.css")
	s := string(b)
	end := strings.Index(s, "*/")
	if !strings.HasPrefix(strings.TrimSpace(s), "/*") || end < 0 {
		t.Fatal("kit.css must open with its header comment")
	}
	header := s[:end]
	for _, n := range []string{"bg", "panel", "card", "fg", "muted", "line", "signal", "signal-ink", "agent", "wait", "danger", "ok", "lift"} {
		if !strings.Contains(header, "--kit-"+n) {
			t.Errorf("header does not give --kit-%s its meaning", n)
		}
	}
}
