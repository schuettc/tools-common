package threadstore

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/subaud-tools/tools-common/msgstate"
)

// The one delivery rule (DCSN5), one test per thing the review found.

func setOwner(t *testing.T, s *Store, scope, session string) []int64 {
	t.Helper()
	moved, err := s.SetOwner(ctx, scope, session)
	if err != nil {
		t.Fatal(err)
	}
	return moved
}

func TestASecondSendReachesASessionHoldingTheFirst(t *testing.T) {
	s, _ := newStore(t)
	th := thread(t, s, "group:x", "triage", "s1")
	first := post(t, s, th, "one")
	d1 := claim(t, s, "s1", "s1")
	second := post(t, s, th, "two")
	d2 := claim(t, s, "s1", "s1")
	if d1 == nil || d2 == nil || d1.ID == d2.ID {
		t.Fatalf("deliveries %+v %+v", d1, d2)
	}
	if !sameIDs(ids(d1.Messages), []int64{first.ID}) || !sameIDs(ids(d2.Messages), []int64{second.ID}) {
		t.Fatalf("messages %v %v", ids(d1.Messages), ids(d2.Messages))
	}
	if got, _ := s.Delivery(ctx, d1.ID); got.State != InFlight {
		t.Fatalf("the first left flight: %+v", got)
	}
}

func TestTwoSendsWaitingAreTwoDeliveries(t *testing.T) {
	s, _ := newStore(t)
	th := thread(t, s, "round:1", "a.md", "s1")
	draft(t, s, th, "one")
	batch, _, _ := s.DraftBatch(ctx, "round:1")
	if _, err := s.SendBatch(ctx, batch); err != nil {
		t.Fatal(err)
	}
	post(t, s, th, "two")
	d1, d2 := claim(t, s, "s1", "s1"), claim(t, s, "s1", "s1")
	if d1 == nil || d2 == nil || len(d1.Messages) != 1 || len(d2.Messages) != 1 {
		t.Fatalf("deliveries %+v %+v", d1, d2)
	}
}

func TestClaimTakesOnlyFromItsScopes(t *testing.T) {
	s, c := newStore(t)
	post(t, s, thread(t, s, "project:other", "t", ""), "not yours")
	post(t, s, thread(t, s, "project:gone", "t", "s9"), "not yours either")
	c.add(DefaultPresenceTTL + time.Second)
	if d := claimIn(t, s, "s1", []string{"project:mine"}, "s1"); d != nil {
		t.Fatalf("took another project's send: %+v", d)
	}
	if d := claimIn(t, s, "s1", nil, "s1"); d != nil {
		t.Fatalf("no scopes took %+v", d)
	}
	if d := claimIn(t, s, "s1", []string{"project:other"}, "s1"); d == nil {
		t.Fatal("got nothing from a scope it may claim")
	}
}

// A session's own scopes come first, even when another's send is older.
func TestClaimTakesItsOwnScopeFirst(t *testing.T) {
	s, c := newStore(t)
	older := post(t, s, thread(t, s, "project:gone", "t", "s9"), "older")
	c.add(DefaultPresenceTTL + time.Second)
	mine := post(t, s, thread(t, s, "project:mine", "t", "s1"), "newer")
	first := claim(t, s, "s1", "s1")
	if first == nil || first.Messages[0].ID != mine.ID {
		t.Fatalf("first %+v", first)
	}
	second := claim(t, s, "s1", "s1")
	if second == nil || second.Messages[0].ID != older.ID {
		t.Fatalf("second %+v", second)
	}
}

// A Send pressed for one owner goes to the owner now, not the one it was
// pressed for.
func TestAQueuedSendGoesToTheOwnerNow(t *testing.T) {
	s, _ := newStore(t)
	m := post(t, s, thread(t, s, "project:p", "t", "a"), "one")
	setOwner(t, s, "project:p", "b")
	if d := claim(t, s, "a", "a", "b"); d != nil {
		t.Fatalf("the old owner got %+v", d)
	}
	d := claim(t, s, "b", "a", "b")
	if d == nil || d.Messages[0].ID != m.ID {
		t.Fatalf("the new owner got %+v", d)
	}
}

// Taking over brings what the old owner never answered, from wherever it got
// to, and leaves what it did answer.
func TestTakingOverBringsTheUnanswered(t *testing.T) {
	s, _ := newStore(t)
	scope := "project:p"
	th := thread(t, s, scope, "t", "a")
	done := post(t, s, th, "answered")
	dDone := claim(t, s, "a", "a")
	if _, err := s.Reply(ctx, "a", []int64{done.ID}, msgstate.Answered, "yes", nil); err != nil {
		t.Fatal(err)
	}
	noReply := post(t, s, th, "no reply")
	dNo := claim(t, s, "a", "a")
	if _, err := s.Landed(ctx, "a", dNo.Token); err != nil {
		t.Fatal(err)
	}
	if _, err := s.TurnEnded(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	working := post(t, s, th, "working")
	dWork := claim(t, s, "a", "a")
	if _, err := s.Reply(ctx, "a", []int64{working.ID}, msgstate.Working, "on it", nil); err != nil {
		t.Fatal(err)
	}
	unlanded := post(t, s, th, "unlanded")
	dSent := claim(t, s, "a", "a")
	queued := post(t, s, th, "queued")

	moved := setOwner(t, s, scope, "b")
	if !sameIDs(moved, []int64{dWork.ID, dSent.ID}) {
		t.Fatalf("moved %v", moved)
	}
	if got, _ := s.Delivery(ctx, dDone.ID); got.State != Done {
		t.Fatalf("a finished delivery changed: %s", got.State)
	}
	if got, _ := s.Delivery(ctx, dSent.ID); got.State != Moved {
		t.Fatalf("delivery %s", got.State)
	}
	if got := stateOf(t, s, done.ID); got != msgstate.Answered {
		t.Fatalf("answered became %s", got)
	}
	if moved, _ := s.Landed(ctx, "a", dSent.Token); len(moved) != 0 {
		t.Fatalf("the old token moved %v", moved)
	}
	if d := claim(t, s, "a", "a", "b"); d != nil {
		t.Fatalf("the old owner got %+v", d)
	}
	var got []int64
	for {
		d := claim(t, s, "b", "a", "b")
		if d == nil {
			break
		}
		got = append(got, ids(d.Messages)...)
	}
	if want := []int64{noReply.ID, working.ID, unlanded.ID, queued.ID}; !sameIDs(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if m, _ := s.Message(ctx, working.ID); m.State != msgstate.Sent || m.Line != "" {
		t.Fatalf("working message %+v", m)
	}
}

// The new owner answers what it took; the old owner no longer can.
func TestAfterATakeoverOnlyTheNewOwnerReplies(t *testing.T) {
	s, _ := newStore(t)
	m := post(t, s, thread(t, s, "project:p", "t", "a"), "one")
	d := claim(t, s, "a", "a")
	if _, err := s.Landed(ctx, "a", d.Token); err != nil {
		t.Fatal(err)
	}
	setOwner(t, s, "project:p", "b")
	if _, err := s.Reply(ctx, "a", []int64{m.ID}, msgstate.Answered, "mine", nil); err == nil {
		t.Fatal("the old owner replied while it was queued")
	}
	claim(t, s, "b", "a", "b")
	if _, err := s.Reply(ctx, "a", []int64{m.ID}, msgstate.Answered, "mine", nil); err == nil {
		t.Fatal("the old owner replied after the new one took it")
	}
	if _, err := s.Reply(ctx, "b", []int64{m.ID}, msgstate.Answered, "done", nil); err != nil {
		t.Fatal(err)
	}
}

// sift and cull name the same owner each time a review opens: nothing it holds
// goes out again.
func TestNamingTheSameOwnerMovesNothing(t *testing.T) {
	s, _ := newStore(t)
	th := thread(t, s, "project:p", "t", "a")
	m1 := post(t, s, th, "landed")
	d1 := claim(t, s, "a", "a")
	if _, err := s.Landed(ctx, "a", d1.Token); err != nil {
		t.Fatal(err)
	}
	m2 := post(t, s, th, "ended")
	d2 := claim(t, s, "a", "a")
	if _, err := s.Landed(ctx, "a", d2.Token); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Reply(ctx, "a", []int64{m1.ID}, msgstate.Working, "on it", nil); err != nil {
		t.Fatal(err)
	}
	if moved := setOwner(t, s, "project:p", "a"); len(moved) != 0 {
		t.Fatalf("moved %v", moved)
	}
	if st := stateOf(t, s, m1.ID); st != msgstate.Working {
		t.Fatalf("m1 %s", st)
	}
	if st := stateOf(t, s, m2.ID); st != msgstate.WithAgent {
		t.Fatalf("m2 %s", st)
	}
	if d := claim(t, s, "a", "a"); d != nil {
		t.Fatalf("delivered again: %+v", d)
	}
}

// One owner per scope: threads in it can't send a batch to two sessions.
func TestABatchIsNeverSplit(t *testing.T) {
	s, c := newStore(t)
	a := thread(t, s, "round:1", "a.md", "a")
	b := thread(t, s, "round:1", "b.md", "b") // the scope's owner is now b
	d1, d2 := draft(t, s, a, "one"), draft(t, s, b, "two")
	batch, _, _ := s.DraftBatch(ctx, "round:1")
	if _, err := s.SendBatch(ctx, batch); err != nil {
		t.Fatal(err)
	}
	if d := claim(t, s, "a", "a", "b"); d != nil {
		t.Fatalf("a took part of it: %+v", d)
	}
	d := claim(t, s, "b", "a", "b")
	if d == nil || !sameIDs(ids(d.Messages), []int64{d1.ID, d2.ID}) {
		t.Fatalf("delivery %+v", d)
	}
	// Taken back, it is still whole.
	c.add(DefaultPresenceTTL + time.Second)
	back := claim(t, s, "c", "c")
	if back == nil || back.ID != d.ID || !sameIDs(ids(back.Messages), []int64{d1.ID, d2.ID}) {
		t.Fatalf("taken back %+v", back)
	}
}

// A owner that returns gets back what a session since gone was holding, and
// another scope's owner doesn't stand in its way.
func TestAReturnedOwnerReclaims(t *testing.T) {
	s, c := newStore(t)
	ma := post(t, s, thread(t, s, "project:a", "t", "a"), "for a")
	mb := post(t, s, thread(t, s, "project:b", "t", "b"), "for b")
	c.add(DefaultPresenceTTL + time.Second)
	if d := claim(t, s, "c", "c"); d == nil {
		t.Fatal("c got nothing")
	}
	claim(t, s, "c", "c")
	c.add(DefaultPresenceTTL + time.Second)
	back := claim(t, s, "a", "a", "b")
	if back == nil || !sameIDs(ids(back.Messages), []int64{ma.ID}) {
		t.Fatalf("a got back %+v", back)
	}
	other := claim(t, s, "b", "a", "b")
	if other == nil || !sameIDs(ids(other.Messages), []int64{mb.ID}) {
		t.Fatalf("b got back %+v", other)
	}
}

// A caller whose present was read before a session became owner doesn't take
// that session's scope from it: "gone" takes PresenceTTL to be believed.
func TestAStaleSnapshotDoesNotTakeFromAFreshOwner(t *testing.T) {
	s, c := newStore(t)
	th := thread(t, s, "project:p", "t", "new")
	post(t, s, th, "one")
	if d := claim(t, s, "other", "other"); d != nil {
		t.Fatalf("took from a fresh owner: %+v", d)
	}
	c.add(DefaultPresenceTTL + time.Second)
	if d := claim(t, s, "other", "other"); d == nil {
		t.Fatal("an owner really gone still holds it")
	}
}

func TestAReplyIsRefusedWhole(t *testing.T) {
	s, _ := newStore(t)
	th := thread(t, s, "group:x", "triage", "s1")
	settled, open := post(t, s, th, "settled"), post(t, s, th, "open")
	claim(t, s, "s1", "s1")
	claim(t, s, "s1", "s1")
	if _, err := s.Reply(ctx, "s1", []int64{settled.ID}, msgstate.Declined, "no", nil); err != nil {
		t.Fatal(err)
	}
	_, err := s.Reply(ctx, "s1", []int64{open.ID, settled.ID}, msgstate.Answered, "yes", nil)
	if err == nil {
		t.Fatal("a reply with a message it can't move went through")
	}
	if got := stateOf(t, s, open.ID); got != msgstate.Sent {
		t.Fatalf("a refused reply moved open to %s", got)
	}
	if rs := replies(t, s, th); len(rs) != 1 {
		t.Fatalf("rows %+v", rs)
	}
}

func TestAReplyKeepsItsIdempotentCases(t *testing.T) {
	s, _ := newStore(t)
	th := thread(t, s, "group:x", "triage", "s1")
	done, open := post(t, s, th, "done"), post(t, s, th, "open")
	claim(t, s, "s1", "s1")
	claim(t, s, "s1", "s1")
	if _, err := s.Reply(ctx, "s1", []int64{done.ID}, msgstate.Answered, "yes", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Reply(ctx, "s1", []int64{open.ID}, msgstate.Working, "first", nil); err != nil {
		t.Fatal(err)
	}
	touched, err := s.Reply(ctx, "s1", []int64{done.ID, open.ID}, msgstate.Answered, "both", nil)
	if err != nil || !sameIDs(touched, []int64{open.ID}) {
		t.Fatalf("touched %v %v", touched, err)
	}
	if rs := replies(t, s, th); len(rs) != 2 {
		t.Fatalf("rows %+v", rs)
	}
}

func TestAnInterruptedDeliveryMovesItsMessages(t *testing.T) {
	s, _ := newStore(t)
	th := thread(t, s, "group:x", "triage", "s1")
	m := post(t, s, th, "one")
	d := claim(t, s, "s1", "s1")
	if _, err := s.Interrupt(ctx); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Delivery(ctx, d.ID); !sameIDs(ids(got.Messages), []int64{m.ID}) {
		t.Fatalf("the interrupted delivery lost its messages: %+v", got)
	}
	if err := s.MoveDelivery(ctx, d.ID, "s2"); err != nil {
		t.Fatal(err)
	}
	if got := thread(t, s, "group:x", "triage", ""); got.Owner != "s2" {
		t.Fatalf("owner %q", got.Owner)
	}
	if got := claim(t, s, "s1", "s1", "s2"); got != nil {
		t.Fatalf("the old session got %+v", got)
	}
	got := claim(t, s, "s2", "s1", "s2")
	if got == nil || !sameIDs(ids(got.Messages), []int64{m.ID}) {
		t.Fatalf("delivery %+v", got)
	}
	if err := s.MoveDelivery(ctx, d.ID, "s3"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("moved a moved delivery: %v", err)
	}
}

func TestTwoClaimersNeverBothGetASend(t *testing.T) {
	s, _ := newStore(t)
	post(t, s, thread(t, s, "group:x", "triage", ""), "one")
	scopes := []string{"group:x"}
	got := make([]*Delivery, 8)
	var wg sync.WaitGroup
	for i := range got {
		wg.Go(func() {
			d, err := s.Claim(ctx, "s"+string(rune('a'+i)), scopes, []string{"sa", "sb", "sc", "sd", "se", "sf", "sg", "sh"})
			if err != nil {
				t.Error(err)
			}
			got[i] = d
		})
	}
	wg.Wait()
	n := 0
	for _, d := range got {
		if d != nil {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("%d claimers got it", n)
	}
}
