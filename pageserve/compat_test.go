package pageserve

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The two shapes on disk today, as their tools declare them: cull's serve
// Advert (tackle internal/cull/serve/advert.go; sift's is the same, diazo's
// adds stale) and galley 0.17.0's registry Entry (galley
// internal/registry/registry.go). An old binary of each keeps reading the
// files a new one writes, and the other way round, so both directions are
// pinned here.
type cullAdvert struct {
	URL       string    `json:"url"`
	Base      string    `json:"base"`
	Token     string    `json:"token"`
	PID       int       `json:"pid"`
	Version   string    `json:"version"`
	StartedAt time.Time `json:"started_at"`
}

type galleyEntry struct {
	V     int    `json:"v"`
	URL   string `json:"url"`
	Room  string `json:"room"`
	Page  string `json:"page"`
	PID   int    `json:"pid"`
	Owner string `json:"owner,omitempty"`
}

// testdata/cull-serve.json is ~/.local/state/cull/live/serve.json and
// testdata/creel-00000000.json one of ~/.galley/live/*.json, copied from a
// real machine with the token, the room and the owner replaced.
func readFixture(t *testing.T, name string, v any) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, v); err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestACullAdvertReadsAsAnEntry(t *testing.T) {
	var old cullAdvert
	raw := readFixture(t, "cull-serve.json", &old)
	var e Entry
	if err := json.Unmarshal(raw, &e); err != nil {
		t.Fatal(err)
	}
	want := Entry{URL: old.URL, Base: old.Base, Token: old.Token, PID: old.PID, Version: old.Version, StartedAt: old.StartedAt}
	if e != want || old.Token == "" || old.StartedAt.IsZero() {
		t.Fatalf("entry = %+v\nwant    %+v", e, want)
	}
}

func TestAGalleyEntryReadsAsAnEntry(t *testing.T) {
	var old galleyEntry
	raw := readFixture(t, "creel-00000000.json", &old)
	var e Entry
	if err := json.Unmarshal(raw, &e); err != nil {
		t.Fatal(err)
	}
	want := Entry{V: old.V, URL: old.URL, Room: old.Room, Page: old.Page, PID: old.PID, Owner: old.Owner}
	if e != want || old.Owner == "" || old.V != 1 {
		t.Fatalf("entry = %+v\nwant    %+v", e, want)
	}
}

// A one-page tool's entry written by this package, read by today's cull.
func TestAnAdvertisedEntryReadsAsACullAdvert(t *testing.T) {
	l := Live{Tool: "cull", Dir: t.TempDir()}
	at := time.Date(2026, 10, 19, 20, 57, 20, 203358000, time.UTC)
	e := Entry{URL: "http://127.0.0.1:62344/?t=x", Base: "http://127.0.0.1:62344", Token: "x", PID: 55796, Version: "0.3.0", StartedAt: at}
	if err := l.Advertise(e); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(l.Dir, "serve.json"))
	if err != nil {
		t.Fatal(err)
	}
	var old cullAdvert
	if err := json.Unmarshal(raw, &old); err != nil {
		t.Fatal(err)
	}
	if want := (cullAdvert{e.URL, e.Base, e.Token, e.PID, e.Version, at}); old != want {
		t.Fatalf("cull reads %+v\nwant         %+v", old, want)
	}
}

// galley's entry written by this package, read by galley 0.17.0, with no key
// galley never wrote.
func TestAWrittenEntryReadsAsAGalleyEntry(t *testing.T) {
	l, dir := tempLive(t)
	e := Entry{URL: "http://127.0.0.1:52360", Room: "creel-00000000", Page: "/tmp/galley-creel/creel.md", PID: os.Getpid(), Owner: "sess-1"}
	if err := l.Write(e); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "creel-00000000.json"))
	if err != nil {
		t.Fatal(err)
	}
	var old galleyEntry
	if err := json.Unmarshal(raw, &old); err != nil {
		t.Fatal(err)
	}
	if want := (galleyEntry{1, e.URL, e.Room, e.Page, e.PID, e.Owner}); old != want {
		t.Fatalf("galley reads %+v\nwant           %+v", old, want)
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(raw, &keys); err != nil {
		t.Fatal(err)
	}
	if len(keys) != 6 {
		t.Fatalf("galley's entry gained keys: %s", raw)
	}
}
