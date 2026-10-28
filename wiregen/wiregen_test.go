package wiregen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/subaud-tools/tools-common/msgstate"
	"github.com/subaud-tools/tools-common/wiregen/internal/fixture/pkga"
	"github.com/subaud-tools/tools-common/wiregen/internal/fixture/pkgb"
)

type base struct {
	ID string `json:"id"`
}

type hidden struct {
	Secret string `json:"secret"`
}

type stage string

func (stage) Values() []string {
	return []string{"start", "understanding", "decisions", "spec", "plans", "build"}
}

type pendingView struct {
	base
	*hidden
	Meta    base                 `json:"meta"`
	Changes []changeView         `json:"changes"`
	Blocks  []changeView         `json:"blocks,omitempty"`
	Later   time.Time            `json:"later,omitzero"`
	Region  *pkga.Region         `json:"region"`
	At      time.Time            `json:"at"`
	Kind    pkga.Kind            `json:"kind"`
	State   msgstate.State       `json:"state"`
	Stage   stage                `json:"stage"`
	Counts  map[string]int       `json:"counts"`
	ByKey   map[string]pkga.Kind `json:"by_key,omitempty"`
	Skipped string               `json:"-"`
	Bare    bool
	_       int // unexported: never written
}

type changeView struct {
	Text string  `json:"text"`
	Size float64 `json:"size"`
}

const header = "// generated\n"

func generate(t *testing.T, roots ...any) string {
	t.Helper()
	out, err := Generate(roots, Options{Header: header})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// The whole file for one root, so every rule of the mapping is pinned: the
// order types are first reached, promotion of embedded fields, optional and
// nullable, time, enums short and past 80 columns, msgstate's name, maps.
func TestGenerateDeclaresWhatEncodingJSONWrites(t *testing.T) {
	want := header + `
export interface PendingView {
  id: string;
  secret: string;
  meta: Base;
  changes: ChangeView[] | null;
  blocks?: ChangeView[];
  later?: string;
  region: Region | null;
  at: string;
  kind: Kind;
  state: MessageState;
  stage: Stage;
  counts: Record<string, number> | null;
  by_key?: Record<string, Kind>;
  Bare: boolean;
}

export interface Base {
  id: string;
}

export interface ChangeView {
  text: string;
  size: number;
}

export interface Region {
  start: number;
  end: number;
}

export type Kind = 'open' | 'decided' | 'reopen';

export type MessageState =
  | 'sent'
  | 'with_agent'
  | 'working'
  | 'answered'
  | 'declined'
  | 'failed'
  | 'no_reply';

export type Stage =
  | 'start'
  | 'understanding'
  | 'decisions'
  | 'spec'
  | 'plans'
  | 'build';
`
	if got := generate(t, pendingView{}); got != want {
		t.Errorf("generated:\n%s\n%s", got, FirstDifference(got, want))
	}
}

func TestARootIsDeclaredOnceAndRootsKeepTheirOrder(t *testing.T) {
	out := generate(t, changeView{}, pkga.Region{}, changeView{})
	if strings.Count(out, "export interface ChangeView {") != 1 {
		t.Errorf("ChangeView declared more than once:\n%s", out)
	}
	if strings.Index(out, "ChangeView {") > strings.Index(out, "Region {") {
		t.Errorf("roots out of order:\n%s", out)
	}
}

// Declaring a type named Record shadows TypeScript's Record<K, V>, so every
// map in that file is an index signature.
func TestMapsAreIndexSignaturesWhenRecordIsDeclared(t *testing.T) {
	type view struct {
		Record pkgb.Record          `json:"record"`
		Kinds  map[string]pkga.Kind `json:"kinds"`
	}
	out := generate(t, view{})
	for _, want := range []string{
		"  record: Record;\n",
		"  kinds: { [key: string]: Kind } | null;\n",
		"export interface Record {\n  counts: { [key: string]: number } | null;\n}\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Record<") {
		t.Errorf("a map is Record<> while Record is declared:\n%s", out)
	}
}

func TestTwoGoTypesWithOneTypeScriptNameAreRefused(t *testing.T) {
	type view struct {
		A pkga.Clash `json:"a"`
		B pkgb.Clash `json:"b"`
	}
	for _, roots := range [][]any{{pkga.Clash{}, pkgb.Clash{}}, {view{}}} {
		_, err := Generate(roots, Options{})
		if err == nil {
			t.Fatal("two types named Clash: want an error")
		}
		for _, want := range []string{`"Clash"`, "fixture/pkga.Clash", "fixture/pkgb.Clash"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error %q does not name %s", err, want)
			}
		}
	}
}

func TestAFieldWithNoShapeIsRefusedUnlessAllowed(t *testing.T) {
	type view struct {
		Payload any               `json:"payload"`
		Inline  struct{ X int }   `json:"inline"`
		Fn      func()            `json:"-"`
		Ok      map[string]string `json:"ok"`
	}
	_, err := Generate([]any{view{}}, Options{})
	if err == nil || !strings.Contains(err.Error(), "View.payload") || !strings.Contains(err.Error(), "View.inline") {
		t.Fatalf("want an error naming View.payload and View.inline, got %v", err)
	}
	out, err := Generate([]any{view{}}, Options{AllowUnknown: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"  payload: unknown | null;\n", "  inline: unknown;\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "any") {
		t.Errorf("the wire carries any:\n%s", out)
	}
}

func TestCheckCommitted(t *testing.T) {
	roots := []any{changeView{}}
	path := filepath.Join(t.TempDir(), "web", "wire.d.ts")
	opts := Options{Header: header, Regenerate: "just wire"}

	err := CheckCommitted(path, roots, opts, false)
	if err == nil || !strings.Contains(err.Error(), "missing") || !strings.Contains(err.Error(), "`just wire`") {
		t.Fatalf("missing file: got %v", err)
	}
	if err := CheckCommitted(path, roots, opts, true); err != nil {
		t.Fatalf("update: %v", err)
	}
	if err := CheckCommitted(path, roots, opts, false); err != nil {
		t.Fatalf("after update: %v", err)
	}

	committed, _ := os.ReadFile(path)
	stale := strings.Replace(string(committed), "size: number;", "bytes: number;", 1)
	if err := os.WriteFile(path, []byte(stale), 0o644); err != nil {
		t.Fatal(err)
	}
	err = CheckCommitted(path, roots, opts, false)
	if err == nil {
		t.Fatal("drift: want an error")
	}
	for _, want := range []string{"`just wire`", "line 5", `"  bytes: number;"`, `"  size: number;"`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("drift error does not name %s:\n%v", want, err)
		}
	}
	if err := CheckCommitted(path, roots, Options{}, false); err == nil || !strings.Contains(err.Error(), "`go test -update`") {
		t.Errorf("no Regenerate: want go test -update named, got %v", err)
	}
}

func TestFirstDifferenceOfFilesThatDifferOnlyInLength(t *testing.T) {
	if got := FirstDifference("a\nb", "a\nb\nc"); !strings.Contains(got, "line 3") {
		t.Errorf("got %q", got)
	}
	if got := FirstDifference("a\n", "a"); got != "(files differ only in length)" {
		t.Errorf("got %q", got)
	}
}
