package threadstore

// The two shapes the store has to hold, start to finish, ported from those
// tools' own tests: casebook's thread with one session (deliver_test.go,
// lifecycle_test.go, acknowledge_test.go) and sift's send across a round
// (sends_test.go, loop_test.go).

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/subaud-tools/tools-common/msgstate"
)

// sendRound drafts one message per file into the round's batch and sends it,
// the way sift's Send records one send for a press of the button.
func sendRound(t *testing.T, s *Store, round string, files ...string) []Message {
	t.Helper()
	var out []Message
	for _, f := range files {
		out = append(out, draft(t, s, thread(t, s, round, f, ""), "accept "+f))
	}
	batch, _, err := s.DraftBatch(ctx, round)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SendBatch(ctx, batch); err != nil {
		t.Fatal(err)
	}
	return out
}

// casebook's thread: one scope to a thread, so its draft tray and its owner are
// the thread's. Its session holds a delivery while the next arrives, and its
// page moves a stuck delivery, which takes the thread with it.
func TestCasebookThread(t *testing.T) {
	s, c := newStore(t)
	scope := "thread:triage"
	th := thread(t, s, scope, "triage", "s1")

	attached := json.RawMessage(`{"keys":["pr:673"],"open":"pr:671"}`)
	m1, err := s.Post(ctx, th.ID, NewMessage{Author: "court", Body: "propose decisions", Meta: attached})
	if err != nil {
		t.Fatal(err)
	}
	c.add(time.Minute)
	d1, d2 := draft(t, s, th, "second draft"), draft(t, s, th, "first draft")
	batch, _, _ := s.DraftBatch(ctx, scope)
	if err := s.EditDraft(ctx, d1.ID, "the second draft"); err != nil {
		t.Fatal(err)
	}
	if err := s.ReorderBatch(ctx, batch, []int64{d2.ID, d1.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SendBatch(ctx, batch); err != nil {
		t.Fatal(err)
	}

	// Another session can't take them while s1 is here.
	if got := claim(t, s, "s2", "s1", "s2"); got != nil {
		t.Fatalf("s2 took s1's messages: %+v", got)
	}

	// Each Send is its own delivery, and the session holds both.
	first := claim(t, s, "s1", "s1", "s2")
	second := claim(t, s, "s1", "s1", "s2")
	if first == nil || second == nil || !sameIDs(ids(first.Messages), []int64{m1.ID}) || !sameIDs(ids(second.Messages), []int64{d2.ID, d1.ID}) {
		t.Fatalf("deliveries %+v %+v", first, second)
	}
	if string(first.Messages[0].Meta) != string(attached) {
		t.Fatalf("meta %s", first.Messages[0].Meta)
	}
	if marked, err := s.Held(ctx, "s1", first.Token); err != nil || !marked {
		t.Fatalf("held %v %v", marked, err)
	}
	if _, err := s.Landed(ctx, "s1", first.Token); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Landed(ctx, "s1", second.Token); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Reply(ctx, "s1", []int64{m1.ID}, msgstate.Working, "reading the PRs", nil); err != nil {
		t.Fatal(err)
	}

	// A message sent mid-turn goes out at once, and has not landed when the turn
	// ends, so it stays in flight for the next.
	mid := post(t, s, th, "actually skip #673")
	stuck := claim(t, s, "s1", "s1", "s2")
	if stuck == nil || !sameIDs(ids(stuck.Messages), []int64{mid.ID}) {
		t.Fatalf("mid-turn delivery %+v", stuck)
	}
	if _, err := s.Reply(ctx, "s1", []int64{m1.ID}, msgstate.Answered, "38 close, 2 keep", nil); err != nil {
		t.Fatal(err)
	}
	ended, err := s.TurnEnded(ctx, "s1")
	if err != nil || len(ended) != 1 || ended[0].ID != second.ID {
		t.Fatalf("turn ended %+v %v", ended, err)
	}
	if got := stateOf(t, s, d2.ID); got != msgstate.NoReply {
		t.Fatalf("d2 %s", got)
	}
	if _, err := s.Reply(ctx, "s1", []int64{d2.ID}, msgstate.Answered, "late", nil); err != nil {
		t.Fatal(err)
	}

	// It never lands: it sticks, Court releases it and sends it again, then
	// moves it. d1, the one no reply left, goes with the thread.
	c.add(DefaultStuckAfter + time.Second)
	if got, _ := s.Stuck(ctx); got[stuck.ID] != "s1" {
		t.Fatalf("stuck %v", got)
	}
	if err := s.Release(ctx, stuck.ID); err != nil {
		t.Fatal(err)
	}
	if n, err := s.Resend(ctx, []int64{mid.ID}); err != nil || n != 1 {
		t.Fatalf("resend %d %v", n, err)
	}
	again := claim(t, s, "s1", "s1", "s2")
	if err := s.MoveDelivery(ctx, again.ID, "s2"); err != nil {
		t.Fatal(err)
	}
	if got := thread(t, s, scope, "triage", ""); got.Owner != "s2" {
		t.Fatalf("owner %q", got.Owner)
	}
	left := claim(t, s, "s2", "s1", "s2")
	if left == nil || !sameIDs(ids(left.Messages), []int64{d1.ID}) {
		t.Fatalf("the unanswered %+v", left)
	}
	if _, err := s.Reply(ctx, "s2", []int64{d1.ID}, msgstate.Answered, "taken", nil); err != nil {
		t.Fatal(err)
	}
	moved := claim(t, s, "s2", "s1", "s2")
	if moved == nil || !sameIDs(ids(moved.Messages), []int64{mid.ID}) {
		t.Fatalf("moved %+v", moved)
	}

	// The server restarts: what was in flight goes out again.
	in, err := s.Interrupt(ctx)
	if err != nil || len(in) != 1 || in[0].Session != "s2" || !sameIDs(in[0].Messages, []int64{mid.ID}) {
		t.Fatalf("interrupted %+v %v", in, err)
	}
	last := claim(t, s, "s2", "s1", "s2")
	if _, err := s.Reply(ctx, "s2", []int64{mid.ID}, msgstate.Declined, "won't: already merged", nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Delivery(ctx, last.ID); got.State != Done {
		t.Fatalf("last delivery %s", got.State)
	}

	// A gone owner's thread passes to another present session.
	c.add(DefaultPresenceTTL + time.Second)
	after := post(t, s, th, "while s2 is away")
	if got := claim(t, s, "s1", "s1"); got == nil || !sameIDs(ids(got.Messages), []int64{after.ID}) {
		t.Fatalf("after s2 went %+v", got)
	}

	// A turn's progress summary is the tool's own row beside the conversation.
	worked := json.RawMessage(`{"duration_ms":1200,"lines":[{"text":"reading"}]}`)
	if _, err := s.Note(ctx, th.ID, NewMessage{Author: "s2", Body: "worked 1s", Meta: worked}); err != nil {
		t.Fatal(err)
	}
	all, err := s.Messages(ctx, th.ID)
	if err != nil {
		t.Fatal(err)
	}
	if n := all[len(all)-1]; n.State != "" || string(n.Meta) != string(worked) {
		t.Fatalf("note %+v", n)
	}
	if rs := replies(t, s, th); len(rs) != 5 || rs[0].Body != "38 close, 2 keep" || rs[0].ReplyTo != m1.ID {
		t.Fatalf("rows %+v", rs)
	}
}

func TestSiftSend(t *testing.T) {
	s, c := newStore(t)
	round := "round:1"
	setOwner(t, s, round, "owner")

	// A send across two files of one round: its present owner gets it, nobody
	// else does.
	send := sendRound(t, s, round, "CLAUDE.md", "AGENTS.md")
	if got := claim(t, s, "other", "owner", "other"); got != nil {
		t.Fatalf("other took a present owner's send: %+v", got)
	}
	del := claim(t, s, "owner", "owner", "other")
	if del == nil || !sameIDs(ids(del.Messages), ids(send)) || del.BatchID != send[0].BatchID {
		t.Fatalf("delivery %+v", del)
	}
	if del.Messages[0].State != msgstate.Sent {
		t.Fatalf("claim moved it to %s", del.Messages[0].State)
	}
	if _, err := s.Landed(ctx, "owner", del.Token); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Reply(ctx, "owner", ids(send), msgstate.Answered, "applied, PR opened", nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Delivery(ctx, del.ID); got.State != Done {
		t.Fatalf("delivery %s", got.State)
	}
	for _, f := range []string{"CLAUDE.md", "AGENTS.md"} {
		if rs := replies(t, s, thread(t, s, round, f, "")); len(rs) != 1 || rs[0].Body != "applied, PR opened" {
			t.Fatalf("%s rows %+v", f, rs)
		}
	}

	// The owner has gone: another present session takes the next send.
	c.add(DefaultPresenceTTL + time.Second)
	next := sendRound(t, s, round, "CLAUDE.md", "AGENTS.md")
	taken := claim(t, s, "other", "other")
	if taken == nil || taken.Session != "other" || !sameIDs(ids(taken.Messages), ids(next)) {
		t.Fatalf("taken %+v", taken)
	}

	// That session dies before it lands: the next takes over, and the dead
	// session's token no longer counts.
	if got := claim(t, s, "third", "third"); got != nil {
		t.Fatalf("third took a fresh claim: %+v", got)
	}
	c.add(DefaultPresenceTTL + time.Second)
	back := claim(t, s, "third", "third")
	if back == nil || back.BatchID != taken.BatchID || back.Token == taken.Token {
		t.Fatalf("taken back %+v", back)
	}
	if moved, _ := s.Landed(ctx, "other", taken.Token); len(moved) != 0 {
		t.Fatalf("the dead session's report moved %v", moved)
	}

	// The turn it lands in ends unanswered; a late answer still settles it.
	if _, err := s.Landed(ctx, "third", back.Token); err != nil {
		t.Fatal(err)
	}
	if _, err := s.TurnEnded(ctx, "third"); err != nil {
		t.Fatal(err)
	}
	if got := stateOf(t, s, next[0].ID); got != msgstate.NoReply {
		t.Fatalf("after the turn %s", got)
	}
	if _, err := s.Reply(ctx, "third", ids(next), msgstate.Answered, "late", nil); err != nil {
		t.Fatal(err)
	}
	if got := stateOf(t, s, next[1].ID); got != msgstate.Answered {
		t.Fatalf("after the late answer %s", got)
	}

	// Two sends waiting are two deliveries, both held at once, and one for a
	// present owner stays with it.
	setOwner(t, s, "round:2", "third")
	setOwner(t, s, "round:3", "third")
	setOwner(t, s, "round:4", "fourth")
	a := sendRound(t, s, "round:2", "a.md")
	b := sendRound(t, s, "round:3", "b.md")
	kept := sendRound(t, s, "round:4", "c.md")
	one, two := claim(t, s, "third", "third", "fourth"), claim(t, s, "third", "third", "fourth")
	if one == nil || two == nil || !sameIDs(ids(one.Messages), ids(a)) || !sameIDs(ids(two.Messages), ids(b)) {
		t.Fatalf("two sends %+v %+v", one, two)
	}
	if got := claim(t, s, "third", "third", "fourth"); got != nil {
		t.Fatalf("third took fourth's send %+v", got)
	}
	if got := stateOf(t, s, kept[0].ID); got != msgstate.Sent {
		t.Fatalf("fourth's send %s", got)
	}
}
