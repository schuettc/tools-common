package threadstore

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/subaud-tools/tools-common/msgstate"
	"github.com/subaud-tools/tools-common/sqlitedb"
)

var ctx = context.Background()

type clock struct{ t time.Time }

func (c *clock) now() time.Time      { return c.t }
func (c *clock) add(d time.Duration) { c.t = c.t.Add(d) }

// toolStep is a tool's own schema, with the names the store must not collide with.
var toolStep = sqlitedb.SQL(`CREATE TABLE threads (id INTEGER PRIMARY KEY);
CREATE TABLE messages (id INTEGER PRIMARY KEY);
CREATE TABLE sends (id INTEGER PRIMARY KEY);`)

func openDB(t *testing.T) *sqlitedb.DB {
	t.Helper()
	d, err := sqlitedb.Open(ctx, filepath.Join(t.TempDir(), "tool.db"), sqlitedb.Options{
		Migrations: append([]sqlitedb.Step{toolStep}, Migrations[:1]...),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

func newStore(t *testing.T) (*Store, *clock) {
	t.Helper()
	c := &clock{t: time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC)}
	s := New(openDB(t))
	s.Now = c.now
	return s, c
}

func thread(t *testing.T, s *Store, scope, name, owner string) Thread {
	t.Helper()
	if owner != "" {
		if _, err := s.SetOwner(ctx, scope, owner); err != nil {
			t.Fatal(err)
		}
	}
	th, err := s.Thread(ctx, scope, name)
	if err != nil {
		t.Fatal(err)
	}
	return th
}

func post(t *testing.T, s *Store, th Thread, body string) Message {
	t.Helper()
	m, err := s.Post(ctx, th.ID, NewMessage{Author: "court", Body: body})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestMigrationsSitBesideAToolsOwnTables(t *testing.T) {
	d := openDB(t)
	rows, err := d.QueryContext(ctx, `SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	own := map[string]bool{"threads": true, "messages": true, "sends": true}
	stores := 0
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		switch {
		case own[name]:
		case strings.HasPrefix(name, "thread_"):
			stores++
		default:
			t.Errorf("table %q has no thread_ prefix", name)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if stores == 0 {
		t.Fatal("the store made no tables")
	}
}

func TestAScopeAndNameAreOneThread(t *testing.T) {
	s, _ := newStore(t)
	first := thread(t, s, "round:1", "CLAUDE.md", "s1")
	again := thread(t, s, "round:1", "CLAUDE.md", "")
	if again != first {
		t.Fatalf("again %+v, want %+v", again, first)
	}
	if other := thread(t, s, "round:2", "CLAUDE.md", ""); other.ID == first.ID || other.Owner != "" {
		t.Fatalf("another scope's thread: %+v", other)
	}
}

// The owner belongs to the scope, so every thread in it has the same one.
func TestAThreadsOwnerIsItsScopes(t *testing.T) {
	s, _ := newStore(t)
	a := thread(t, s, "round:1", "a.md", "s1")
	setOwner(t, s, "round:1", "s2")
	if got := thread(t, s, "round:1", "a.md", ""); got.ID != a.ID || got.Owner != "s2" {
		t.Fatalf("thread %+v", got)
	}
}

func TestThreadsListsOneScopeOldestFirst(t *testing.T) {
	s, _ := newStore(t)
	a := thread(t, s, "round:1", "a", "")
	b := thread(t, s, "round:1", "b", "")
	thread(t, s, "round:2", "c", "")
	got, err := s.Threads(ctx, "round:1")
	if err != nil || len(got) != 2 || got[0].ID != a.ID || got[1].ID != b.ID {
		t.Fatalf("threads %+v %v", got, err)
	}
}

func TestPostQueuesAMessageForTheThreadsOwner(t *testing.T) {
	s, c := newStore(t)
	th := thread(t, s, "group:x", "triage", "s1")
	first := post(t, s, th, "first")
	c.add(time.Minute)
	m, err := s.Post(ctx, th.ID, NewMessage{Author: "court", Body: "merge these", Meta: json.RawMessage(`{"keys":["a"]}`), ReplyTo: first.ID})
	if err != nil {
		t.Fatal(err)
	}
	if m.ThreadID != th.ID || m.Author != "court" || m.Body != "merge these" || string(m.Meta) != `{"keys":["a"]}` ||
		m.State != msgstate.Sent || m.ReplyTo != first.ID || !m.CreatedAt.Equal(c.t) || !m.QueuedAt.Equal(c.t) {
		t.Fatalf("message %+v", m)
	}
}

func TestPostRefusesAnEmptyBody(t *testing.T) {
	s, _ := newStore(t)
	th := thread(t, s, "group:x", "triage", "s1")
	if _, err := s.Post(ctx, th.ID, NewMessage{Author: "court", Body: "  "}); err == nil {
		t.Fatal("an empty message was stored")
	}
}

// A row the tool writes outside the lifecycle (casebook's progress summary)
// has no state and never goes out.
func TestNoteHasNoState(t *testing.T) {
	s, _ := newStore(t)
	th := thread(t, s, "group:x", "triage", "s1")
	post(t, s, th, "one")
	n, err := s.Note(ctx, th.ID, NewMessage{Author: "s1", Body: "worked 3 minutes", Meta: json.RawMessage(`{"duration_ms":1}`)})
	if err != nil {
		t.Fatal(err)
	}
	if n.State != "" || n.Author != "s1" || string(n.Meta) != `{"duration_ms":1}` {
		t.Fatalf("note %+v", n)
	}
	all, err := s.Messages(ctx, th.ID)
	if err != nil || len(all) != 2 || all[1].ID != n.ID {
		t.Fatalf("messages %+v %v", all, err)
	}
}
