package page

import (
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strings"
	"testing"
)

var (
	jsFunc   = regexp.MustCompile(`(?m)^export (?:async )?function\*? ?(\w+)`)
	jsDecl   = regexp.MustCompile(`(?m)^export (?:const|let|class) (\w+)`)
	jsList   = regexp.MustCompile(`(?m)^export \{([^}]+)\}`)
	dtsValue = regexp.MustCompile(`(?m)^export (?:declare )?(?:function|const|class) (\w+)`)
	reexport = regexp.MustCompile(`(?m)^export \* from '([^']+)';`)
	specRE   = regexp.MustCompile(`(?m)(?:^import|^export)[^'"\n]*?from\s*['"]([^'"]+)['"]|import\(\s*['"]([^'"]+)['"]`)
)

// modules are the ES modules kit.js re-exports; boot.js is a classic script.
func modules(t *testing.T) []string {
	t.Helper()
	var out []string
	es, _ := fs.ReadDir(FS(), ".")
	for _, e := range es {
		if n := e.Name(); strings.HasSuffix(n, ".js") && n != "kit.js" && n != "boot.js" {
			out = append(out, n)
		}
	}
	return out
}

func read(t *testing.T, name string) string {
	t.Helper()
	b, err := fs.ReadFile(FS(), name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func names(res []*regexp.Regexp, src string) map[string]bool {
	out := map[string]bool{}
	for _, re := range res {
		for _, m := range re.FindAllStringSubmatch(src, -1) {
			for _, n := range strings.Split(m[1], ",") {
				n = strings.TrimSpace(n)
				if i := strings.Index(n, " as "); i >= 0 {
					n = strings.TrimSpace(n[i+4:])
				}
				if n != "" {
					out[n] = true
				}
			}
		}
	}
	return out
}

func keys(m map[string]bool) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func TestDTSMatchesExports(t *testing.T) {
	js := map[string]bool{}
	for _, m := range modules(t) {
		for n := range names([]*regexp.Regexp{jsFunc, jsDecl, jsList}, read(t, m)) {
			js[n] = true
		}
	}
	dts := names([]*regexp.Regexp{dtsValue}, read(t, "kit.d.ts"))
	if strings.Join(keys(js), " ") != strings.Join(keys(dts), " ") {
		t.Errorf("value exports differ:\n  js:   %v\n  d.ts: %v", keys(js), keys(dts))
	}
}

func TestKitReexportsAll(t *testing.T) {
	got := map[string]bool{}
	for _, m := range reexport.FindAllStringSubmatch(read(t, "kit.js"), -1) {
		got[path.Base(m[1])] = true
	}
	for _, m := range modules(t) {
		if !got[m] {
			t.Errorf("kit.js does not re-export %s", m)
		}
	}
}

func TestImportsStayInKit(t *testing.T) {
	ok := regexp.MustCompile(`^\./[a-z]+\.js$`)
	es, _ := fs.ReadDir(FS(), ".")
	for _, e := range es {
		if !strings.HasSuffix(e.Name(), ".js") {
			continue
		}
		for _, m := range specRE.FindAllStringSubmatch(read(t, e.Name()), -1) {
			spec := m[1] + m[2]
			if !ok.MatchString(spec) {
				t.Errorf("%s imports %q; kit assets import only ./<module>.js", e.Name(), spec)
			}
		}
	}
}
