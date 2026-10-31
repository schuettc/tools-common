package threadstore

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/subaud-tools/tools-common/msgstate"
	"github.com/subaud-tools/tools-common/sqlitedb"
)

func batchIDs(bs []Batch) []int64 {
	var out []int64
	for _, b := range bs {
		out = append(out, b.ID)
	}
	return out
}

func TestScopesListsEveryScopeWithAThread(t *testing.T) {
	s, _ := newStore(t)
	thread(t, s, "round:2", "a", "")
	thread(t, s, "round:1", "a", "")
	thread(t, s, "round:1", "b", "")
	setOwner(t, s, "round:3", "s1") // an owner but no thread yet
	got, err := s.Scopes(ctx)
	if err != nil || len(got) != 2 || got[0] != "round:1" || got[1] != "round:2" {
		t.Fatalf("scopes %v %v", got, err)
	}
}

func TestSendCountCountsASendsOfOneScope(t *testing.T) {
	s, _ := newStore(t)
	sendRound(t, s, "round:1", "a.md", "b.md")
	sendRound(t, s, "round:1", "a.md")
	sendRound(t, s, "round:2", "a.md")
	draft(t, s, thread(t, s, "round:1", "a.md", ""), "not sent")
	if n, err := s.SendCount(ctx, "round:1"); err != nil || n != 2 {
		t.Fatalf("count %d %v", n, err)
	}
}

func TestRecentSendsAreNewestFirstAndLimited(t *testing.T) {
	s, c := newStore(t)
	one := sendRound(t, s, "round:1", "a.md")
	c.add(time.Minute)
	two := sendRound(t, s, "round:1", "a.md")
	c.add(time.Minute)
	three := sendRound(t, s, "round:1", "a.md", "b.md")
	sendRound(t, s, "round:2", "a.md")
	got, err := s.RecentSends(ctx, "round:1", 2)
	if err != nil || !sameIDs(batchIDs(got), []int64{three[0].BatchID, two[0].BatchID}) {
		t.Fatalf("recent %v %v, %d", batchIDs(got), err, one[0].BatchID)
	}
	if b := got[0]; b.Scope != "round:1" || !b.SentAt.Equal(c.t) || !sameIDs(ids(b.Messages), ids(three)) {
		t.Fatalf("batch %+v", b)
	}
}

// What a tool's page shows of a Send: who has it, held, and the agent's answer.
func TestABatchSaysWhereItStands(t *testing.T) {
	s, _ := newStore(t)
	setOwner(t, s, "round:1", "s1")
	m := sendRound(t, s, "round:1", "a.md", "b.md")
	read := func() Batch {
		t.Helper()
		b, err := s.Batch(ctx, m[0].BatchID)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	if b := read(); b.ClaimedBy != "" || b.Held {
		t.Fatalf("waiting %+v", b)
	}
	d := claim(t, s, "s1", "s1")
	if _, err := s.Held(ctx, "s1", d.Token); err != nil {
		t.Fatal(err)
	}
	if b := read(); b.ClaimedBy != "s1" || !b.Held {
		t.Fatalf("held %+v", b)
	}
	if _, err := s.Reply(ctx, "s1", ids(m), msgstate.Answered, "applied", nil); err != nil {
		t.Fatal(err)
	}
	b := read()
	if b.ClaimedBy != "s1" || b.Held || len(b.Replies) != 2 || b.Replies[0].Body != "applied" {
		t.Fatalf("answered %+v", b)
	}
	if b.Messages[1].DeliveryID != d.ID {
		t.Fatalf("message's delivery %+v", b.Messages[1])
	}
	setOwner(t, s, "round:1", "s2") // nothing open: it stays as it is
	if b := read(); b.ClaimedBy != "s1" {
		t.Fatalf("after a takeover %+v", b)
	}
}

func TestABatchTakenOverWaitsAgain(t *testing.T) {
	s, _ := newStore(t)
	setOwner(t, s, "round:1", "s1")
	m := sendRound(t, s, "round:1", "a.md")
	d := claim(t, s, "s1", "s1")
	if _, err := s.Held(ctx, "s1", d.Token); err != nil {
		t.Fatal(err)
	}
	setOwner(t, s, "round:1", "s2")
	if b, err := s.Batch(ctx, m[0].BatchID); err != nil || b.ClaimedBy != "" || b.Held {
		t.Fatalf("batch %+v %v", b, err)
	}
}

func TestABatchThatIsNoneIsNotFound(t *testing.T) {
	s, _ := newStore(t)
	if _, err := s.Batch(ctx, 99); err == nil {
		t.Fatal("found a batch that isn't there")
	}
}

func TestUnsettledListsSendsNotSettledInTheScopesGiven(t *testing.T) {
	s, _ := newStore(t)
	setOwner(t, s, "round:1", "s1")
	done := sendRound(t, s, "round:1", "a.md")
	claim(t, s, "s1", "s1")
	if _, err := s.Reply(ctx, "s1", ids(done), msgstate.Answered, "done", nil); err != nil {
		t.Fatal(err)
	}
	noReply := sendRound(t, s, "round:1", "b.md")
	d := claim(t, s, "s1", "s1")
	if _, err := s.Landed(ctx, "s1", d.Token); err != nil {
		t.Fatal(err)
	}
	if _, err := s.TurnEnded(ctx, "s1"); err != nil {
		t.Fatal(err)
	}
	waiting := sendRound(t, s, "round:1", "c.md")
	elsewhere := sendRound(t, s, "round:2", "d.md")
	draft(t, s, thread(t, s, "round:1", "a.md", ""), "not sent")
	got, err := s.Unsettled(ctx, []string{"round:1"})
	if err != nil || !sameIDs(batchIDs(got), []int64{noReply[0].BatchID, waiting[0].BatchID}) {
		t.Fatalf("unsettled %v %v", batchIDs(got), err)
	}
	got, err = s.Unsettled(ctx, []string{"round:1", "round:2"})
	if err != nil || len(got) != 3 || got[2].ID != elsewhere[0].BatchID {
		t.Fatalf("unsettled %v %v", batchIDs(got), err)
	}
}

func TestInFlightListsASessionsDeliveries(t *testing.T) {
	s, _ := newStore(t)
	th := thread(t, s, "group:x", "triage", "s1")
	post(t, s, th, "one")
	post(t, s, th, "two")
	done := post(t, s, th, "three")
	d1, d2, d3 := claim(t, s, "s1", "s1"), claim(t, s, "s1", "s1"), claim(t, s, "s1", "s1")
	if _, err := s.Reply(ctx, "s1", []int64{done.ID}, msgstate.Answered, "done", nil); err != nil {
		t.Fatal(err)
	}
	got, err := s.InFlight(ctx, "s1")
	if err != nil || len(got) != 2 || got[0].ID != d1.ID || got[1].ID != d2.ID || d3.ID == d2.ID || len(got[0].Messages) != 1 {
		t.Fatalf("in flight %+v %v", got, err)
	}
	if got, err := s.InFlight(ctx, "s2"); err != nil || len(got) != 0 {
		t.Fatalf("another session's %+v %v", got, err)
	}
}

func TestPendingCountsWhatWaitsForAnOwner(t *testing.T) {
	s, _ := newStore(t)
	th := thread(t, s, "group:x", "triage", "s1")
	post(t, s, th, "delivered")
	claim(t, s, "s1", "s1")
	post(t, s, th, "one")
	post(t, s, th, "two")
	post(t, s, thread(t, s, "group:y", "other", "s2"), "not s1's")
	draft(t, s, th, "not sent")
	if n, err := s.Pending(ctx, "s1"); err != nil || n != 2 {
		t.Fatalf("pending %d %v", n, err)
	}
}

func TestThreadsByOwnerAndByID(t *testing.T) {
	s, _ := newStore(t)
	a := thread(t, s, "thread:a", "a", "s1")
	b := thread(t, s, "thread:b", "b", "s2")
	c := thread(t, s, "thread:c", "c", "s1")
	nobody := thread(t, s, "thread:d", "d", "")
	got, err := s.ThreadsByOwner(ctx, "s1")
	if err != nil || len(got) != 2 || got[0].ID != a.ID || got[1].ID != c.ID {
		t.Fatalf("s1's %+v %v", got, err)
	}
	all, err := s.ThreadsByOwner(ctx, "")
	if err != nil || len(all) != 4 || all[3].ID != nobody.ID {
		t.Fatalf("all %+v %v", all, err)
	}
	if got, err := s.ThreadByID(ctx, b.ID); err != nil || got != b {
		t.Fatalf("by id %+v %v", got, err)
	}
	if _, err := s.ThreadByID(ctx, 99); err == nil {
		t.Fatal("found a thread that isn't there")
	}
}

func TestPreviousIsTheFinishedDeliveryBefore(t *testing.T) {
	s, _ := newStore(t)
	th := thread(t, s, "group:x", "triage", "s1")
	m1 := post(t, s, th, "finished")
	d1 := claim(t, s, "s1", "s1")
	post(t, s, th, "still in flight")
	d2 := claim(t, s, "s1", "s1")
	post(t, s, th, "now")
	d3 := claim(t, s, "s1", "s1")
	if got, err := s.Previous(ctx, "s1", d1.ID); err != nil || got != nil {
		t.Fatalf("before the first %+v %v", got, err)
	}
	if got, err := s.Previous(ctx, "s1", d3.ID); err != nil || got != nil {
		t.Fatalf("nothing finished %+v %v", got, err)
	}
	if _, err := s.Reply(ctx, "s1", []int64{m1.ID}, msgstate.Answered, "done", nil); err != nil {
		t.Fatal(err)
	}
	got, err := s.Previous(ctx, "s1", d3.ID)
	if err != nil || got == nil || got.ID != d1.ID || d2.ID == got.ID {
		t.Fatalf("previous %+v %v", got, err)
	}
	if other, err := s.Previous(ctx, "s2", d3.ID); err != nil || other != nil {
		t.Fatalf("another session's %+v %v", other, err)
	}
}

// A late hook names a delivery a report already ended: the newer one stays.
func TestTurnEndedForLeavesWhatItDoesNotName(t *testing.T) {
	s, _ := newStore(t)
	th := thread(t, s, "group:x", "triage", "s1")
	post(t, s, th, "old")
	old := claim(t, s, "s1", "s1")
	if _, err := s.Landed(ctx, "s1", old.Token); err != nil {
		t.Fatal(err)
	}
	if _, err := s.TurnEnded(ctx, "s1"); err != nil {
		t.Fatal(err)
	}
	newer := post(t, s, th, "newer")
	d := claim(t, s, "s1", "s1")
	if _, err := s.Landed(ctx, "s1", d.Token); err != nil {
		t.Fatal(err)
	}
	ended, err := s.TurnEndedFor(ctx, "s1", []int64{old.ID})
	if err != nil || len(ended) != 0 {
		t.Fatalf("ended %+v %v", ended, err)
	}
	if got := stateOf(t, s, newer.ID); got != msgstate.WithAgent {
		t.Fatalf("the late hook moved the newer to %s", got)
	}
	if ended, err := s.TurnEndedFor(ctx, "s2", []int64{d.ID}); err != nil || len(ended) != 0 {
		t.Fatalf("another session's hook ended %+v %v", ended, err)
	}
	ended, err = s.TurnEndedFor(ctx, "s1", []int64{d.ID})
	if err != nil || len(ended) != 1 || ended[0].ID != d.ID || ended[0].State != Done {
		t.Fatalf("ended %+v %v", ended, err)
	}
	if got := stateOf(t, s, newer.ID); got != msgstate.NoReply {
		t.Fatalf("newer %s", got)
	}
}

// Naming a delivery says the agent was shown it, whether or not it was reported.
func TestTurnEndedForLandsWhatItNames(t *testing.T) {
	s, _ := newStore(t)
	m := post(t, s, thread(t, s, "group:x", "triage", "s1"), "one")
	d := claim(t, s, "s1", "s1")
	ended, err := s.TurnEndedFor(ctx, "s1", []int64{d.ID})
	if err != nil || len(ended) != 1 || ended[0].ShownAt.IsZero() {
		t.Fatalf("ended %+v %v", ended, err)
	}
	if got := stateOf(t, s, m.ID); got != msgstate.NoReply {
		t.Fatalf("state %s", got)
	}
}

// sqlitedb numbers a database's steps by their place in the tool's list. A tool
// takes the store's steps by position and adds a later one after its own, so a
// database at any earlier version moves forward with nothing renumbered.
func TestAToolTakesTheStoresStepsByPosition(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tool.db")
	own1 := sqlitedb.SQL("CREATE TABLE decisions (id INTEGER PRIMARY KEY)")
	own2 := sqlitedb.SQL("CREATE TABLE rounds (id INTEGER PRIMARY KEY)")
	laterStore := sqlitedb.SQL("CREATE TABLE thread_later (id INTEGER PRIMARY KEY)")
	open := func(steps ...sqlitedb.Step) *sqlitedb.DB {
		t.Helper()
		d, err := sqlitedb.Open(ctx, path, sqlitedb.Options{Migrations: steps})
		if err != nil {
			t.Fatal(err)
		}
		return d
	}

	d := open(own1, Migrations[0])
	s := New(d)
	th, err := s.Thread(ctx, "round:1", "a.md")
	if err != nil {
		t.Fatal(err)
	}
	m, err := s.Post(ctx, th.ID, NewMessage{Author: "court", Body: "kept"})
	if err != nil {
		t.Fatal(err)
	}
	_ = d.Close()

	// The tool's next step goes after what it took, then the store's next.
	d = open(own1, Migrations[0], own2, laterStore)
	defer func() { _ = d.Close() }()
	if v, err := d.Version(ctx); err != nil || v != 4 {
		t.Fatalf("version %d %v", v, err)
	}
	if got, err := New(d).Message(ctx, m.ID); err != nil || got.Body != "kept" {
		t.Fatalf("message %+v %v", got, err)
	}
	for _, table := range []string{"decisions", "rounds", "thread_later", "thread_messages"} {
		var n int
		if err := d.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE name = ?", table).Scan(&n); err != nil || n != 1 {
			t.Fatalf("table %s: %d %v", table, n, err)
		}
	}
}
