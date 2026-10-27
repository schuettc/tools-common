package pageserve

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPortHintRoundTrip(t *testing.T) {
	l, _ := tempLive(t)

	doc := filepath.Join(t.TempDir(), "guide.html")
	if _, ok := l.LastPort(doc); ok {
		t.Fatal("no hint written yet, want (0, false)")
	}

	l.SaveLastPort(doc, 8931)
	got, ok := l.LastPort(doc)
	if !ok || got != 8931 {
		t.Fatalf("LastPort = (%d, %v), want (8931, true)", got, ok)
	}

	// A relative path resolves to the same abs key as the absolute one.
	l.SaveLastPort(doc, 9012)
	if got, ok := l.LastPort(doc); !ok || got != 9012 {
		t.Fatalf("overwrite: LastPort = (%d, %v), want (9012, true)", got, ok)
	}
}

func TestPortHintDistinctDocs(t *testing.T) {
	l, _ := tempLive(t)
	a := filepath.Join(t.TempDir(), "a.md")
	b := filepath.Join(t.TempDir(), "b.md")
	l.SaveLastPort(a, 7001)
	l.SaveLastPort(b, 7002)
	if got, _ := l.LastPort(a); got != 7001 {
		t.Errorf("doc a port = %d, want 7001", got)
	}
	if got, _ := l.LastPort(b); got != 7002 {
		t.Errorf("doc b port = %d, want 7002", got)
	}
}

func TestPortHintRejectsMismatchedPath(t *testing.T) {
	l, _ := tempLive(t)
	doc := filepath.Join(t.TempDir(), "guide.html")
	l.SaveLastPort(doc, 8080)

	// Corrupt the stored path so it no longer matches the file's key: a hash
	// collision must hand back "no hint", never another document's port.
	name, err := l.portHintPath(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte(`{"path":"/somewhere/else.md","port":8080}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := l.LastPort(doc); ok {
		t.Error("mismatched path should read as no hint")
	}
}

// A port hint must never surface in Inspect — not as an entry and not as a
// refused-advert Problem. Before the hints moved into a subdirectory they lived
// beside adverts, so every scan read each one as an advert, failed Validate on
// its missing room, and logged "invalid room" to the channel's stderr.
func TestPortHintInvisibleToInspect(t *testing.T) {
	l, _ := tempLive(t)
	l.SaveLastPort(filepath.Join(t.TempDir(), "guide.md"), 8080)

	entries, problems, err := l.Inspect()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("a port hint became %d entries, want 0", len(entries))
	}
	if len(problems) != 0 {
		t.Errorf("a port hint became %d problems (%v), want 0", len(problems), problems)
	}
}

func TestPortHintRejectsBadPort(t *testing.T) {
	l, _ := tempLive(t)
	doc := filepath.Join(t.TempDir(), "guide.html")
	l.SaveLastPort(doc, 0)
	if _, ok := l.LastPort(doc); ok {
		t.Error("port 0 is not a real port; should read as no hint")
	}
}
