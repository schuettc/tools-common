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

// casebook's messages never pass to another session on their own: a gone
// session's thread waits until Court moves it. It says so by naming every
// session it knows as present.
var casebookSessions = []string{"s1", "s2"}

func TestCasebookThread(t *testing.T) {
	s, c := newStore(t)
	th := thread(t, s, "group:triage", "triage", "s1")

	// A message with what it refers to, then two drafts sent as a batch.
	attached := json.RawMessage(`{"keys":["pr:673"],"open":"pr:671"}`)
	m1, err := s.Post(ctx, th.ID, NewMessage{Author: "court", Body: "propose decisions", Meta: attached})
	if err != nil {
		t.Fatal(err)
	}
	c.add(time.Minute)
	d1, d2 := draft(t, s, th, "second draft"), draft(t, s, th, "first draft")
	batch, _, _ := s.DraftBatch(ctx, "group:triage")
	if err := s.EditDraft(ctx, d1.ID, "the second draft"); err != nil {
		t.Fatal(err)
	}
	if err := s.ReorderBatch(ctx, batch, []int64{d2.ID, d1.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SendBatch(ctx, batch, ""); err != nil {
		t.Fatal(err)
	}

	// Another session can't take them, even once s1 has left.
	if got := claim(t, s, "s2", casebookSessions...); got != nil {
		t.Fatalf("s2 took s1's messages: %+v", got)
	}

	// One delivery carries them all, in order, and goes through its reports.
	del := claim(t, s, "s1", casebookSessions...)
	if del == nil || !sameIDs(ids(del.Messages), []int64{m1.ID, d2.ID, d1.ID}) {
		t.Fatalf("delivery %+v", del)
	}
	if string(del.Messages[0].Meta) != string(attached) {
		t.Fatalf("meta %s", del.Messages[0].Meta)
	}
	if marked, err := s.Held(ctx, "s1", del.Token); err != nil || !marked {
		t.Fatalf("held %v %v", marked, err)
	}
	if _, err := s.Landed(ctx, "s1", del.Token); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Reply(ctx, "s1", []int64{m1.ID}, msgstate.Working, "reading the PRs", nil); err != nil {
		t.Fatal(err)
	}

	// A message sent mid-turn waits.
	mid := post(t, s, th, "actually skip #673")
	if got := claim(t, s, "s1", casebookSessions...); got != nil {
		t.Fatalf("delivered mid-turn: %+v", got)
	}

	// The turn ends with one message answered: the rest are no reply, and a
	// late answer still settles one.
	if _, _, err := s.Reply(ctx, "s1", []int64{m1.ID}, msgstate.Answered, "38 close, 2 keep", nil); err != nil {
		t.Fatal(err)
	}
	if ended, err := s.TurnEnded(ctx, "s1"); err != nil || ended == nil || ended.State != Done {
		t.Fatalf("turn ended %+v %v", ended, err)
	}
	if got := stateOf(t, s, d2.ID); got != msgstate.NoReply {
		t.Fatalf("d2 %s", got)
	}
	if _, _, err := s.Reply(ctx, "s1", []int64{d2.ID}, msgstate.Answered, "late", nil); err != nil {
		t.Fatal(err)
	}

	// The next delivery never lands: it sticks, Court releases it and sends
	// it again, then moves it to another session.
	stuck := claim(t, s, "s1", casebookSessions...)
	if stuck == nil || !sameIDs(ids(stuck.Messages), []int64{mid.ID}) {
		t.Fatalf("next delivery %+v", stuck)
	}
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
	again := claim(t, s, "s1", casebookSessions...)
	if err := s.MoveDelivery(ctx, again.ID, "s2"); err != nil {
		t.Fatal(err)
	}
	moved := claim(t, s, "s2", casebookSessions...)
	if moved == nil || !sameIDs(ids(moved.Messages), []int64{mid.ID}) {
		t.Fatalf("moved %+v", moved)
	}

	// The server restarts: what was in flight goes out again.
	in, err := s.Interrupt(ctx)
	if err != nil || len(in) != 1 || in[0].Session != "s2" || !sameIDs(in[0].Messages, []int64{mid.ID}) {
		t.Fatalf("interrupted %+v %v", in, err)
	}
	last := claim(t, s, "s2", casebookSessions...)
	if _, _, err := s.Reply(ctx, "s2", []int64{mid.ID}, msgstate.Declined, "won't: already merged", nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Delivery(ctx, last.ID); got.State != Done {
		t.Fatalf("last delivery %s", got.State)
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
	if rs := replies(t, s, th); len(rs) != 4 || rs[0].Body != "38 close, 2 keep" || rs[0].ReplyTo != m1.ID {
		t.Fatalf("rows %+v", rs)
	}
}

// sendRound drafts one message per file into the round's batch and sends it
// for owner, the way sift's Send records one send for a press of the button.
func sendRound(t *testing.T, s *Store, round, owner string, files ...string) []Message {
	t.Helper()
	var out []Message
	for _, f := range files {
		out = append(out, draft(t, s, thread(t, s, round, f, ""), "accept "+f))
	}
	batch, _, err := s.DraftBatch(ctx, round)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SendBatch(ctx, batch, owner); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestSiftSend(t *testing.T) {
	s, c := newStore(t)

	// A send across two files of one round: its present owner gets it, nobody
	// else does.
	send := sendRound(t, s, "round:1", "owner", "CLAUDE.md", "AGENTS.md")
	if got := claim(t, s, "other", "owner", "other"); got != nil {
		t.Fatalf("other took a present owner's send: %+v", got)
	}
	del := claim(t, s, "owner", "owner", "other")
	if del == nil || !sameIDs(ids(del.Messages), ids(send)) || del.Messages[0].BatchID != del.Messages[1].BatchID {
		t.Fatalf("delivery %+v", del)
	}
	if del.Messages[0].State != msgstate.Sent {
		t.Fatalf("claim moved it to %s", del.Messages[0].State)
	}
	if _, err := s.Landed(ctx, "owner", del.Token); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Reply(ctx, "owner", ids(send), msgstate.Answered, "applied, PR opened", nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Delivery(ctx, del.ID); got.State != Done {
		t.Fatalf("delivery %s", got.State)
	}
	for _, f := range []string{"CLAUDE.md", "AGENTS.md"} {
		if rs := replies(t, s, thread(t, s, "round:1", f, "")); len(rs) != 1 || rs[0].Body != "applied, PR opened" {
			t.Fatalf("%s rows %+v", f, rs)
		}
	}

	// The owner has gone: another present session takes the next send, and
	// the owner isn't who answers it.
	next := sendRound(t, s, "round:1", "owner", "CLAUDE.md", "AGENTS.md")
	taken := claim(t, s, "other", "other")
	if taken == nil || taken.Session != "other" || !sameIDs(ids(taken.Messages), ids(next)) {
		t.Fatalf("taken %+v", taken)
	}

	// That session dies before it lands: the send isn't stranded, and the dead
	// session's token no longer counts.
	if got := claim(t, s, "third", "third"); got != nil {
		t.Fatalf("third took a fresh claim: %+v", got)
	}
	c.add(DefaultPresenceTTL + time.Second)
	back := claim(t, s, "third", "third")
	if back == nil || back.ID != taken.ID || back.Token == taken.Token {
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
	if _, _, err := s.Reply(ctx, "third", ids(next), msgstate.Answered, "late", nil); err != nil {
		t.Fatal(err)
	}
	if got := stateOf(t, s, next[1].ID); got != msgstate.Answered {
		t.Fatalf("after the late answer %s", got)
	}

	// Two sends waiting go out in one delivery; each stays its own batch, so
	// the tool can tell them apart. One recorded for an owner still here stays
	// with it.
	a := sendRound(t, s, "round:2", "third", "a.md")
	b := sendRound(t, s, "round:3", "third", "b.md")
	kept := sendRound(t, s, "round:4", "fourth", "c.md")
	two := claim(t, s, "third", "third", "fourth")
	if two == nil || !sameIDs(ids(two.Messages), []int64{a[0].ID, b[0].ID}) || two.Messages[0].BatchID == two.Messages[1].BatchID {
		t.Fatalf("two sends %+v", two)
	}
	if got, _ := s.Message(ctx, kept[0].ID); got.Owner != "fourth" || got.State != msgstate.Sent {
		t.Fatalf("fourth's send %+v", got)
	}
}
