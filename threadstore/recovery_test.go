package threadstore

import (
	"errors"
	"testing"
	"time"

	"github.com/subaud-tools/tools-common/msgstate"
)

func TestStuckListsDeliveriesUntouchedForStuckAfter(t *testing.T) {
	s, c := newStore(t)
	post(t, s, thread(t, s, "group:x", "triage", "s1"), "one")
	d := claim(t, s, "s1", "s1")
	if stuck, err := s.Stuck(ctx); err != nil || len(stuck) != 0 {
		t.Fatalf("stuck %v %v", stuck, err)
	}
	c.add(DefaultStuckAfter + time.Second)
	stuck, err := s.Stuck(ctx)
	if err != nil || stuck[d.ID] != "s1" {
		t.Fatalf("stuck %v %v", stuck, err)
	}
	if got, _ := s.Delivery(ctx, d.ID); !got.Stuck {
		t.Fatal("the delivery doesn't say it is stuck")
	}
}

func TestStuckAfterOverridesTheThreshold(t *testing.T) {
	s, c := newStore(t)
	s.StuckAfter = time.Minute
	post(t, s, thread(t, s, "group:x", "triage", "s1"), "one")
	d := claim(t, s, "s1", "s1")
	c.add(2 * time.Minute)
	if stuck, _ := s.Stuck(ctx); stuck[d.ID] != "s1" {
		t.Fatalf("stuck %v", stuck)
	}
}

func sendTwo(t *testing.T, s *Store, scope string) (Thread, Message, Message) {
	t.Helper()
	th := thread(t, s, scope, "triage", "s1")
	m1, m2 := draft(t, s, th, "one"), draft(t, s, th, "two")
	batch, _, _ := s.DraftBatch(ctx, scope)
	if _, err := s.SendBatch(ctx, batch); err != nil {
		t.Fatal(err)
	}
	return th, m1, m2
}

func TestReleaseLandsEveryUnsettledMessageOnNoReply(t *testing.T) {
	s, c := newStore(t)
	_, m1, m2 := sendTwo(t, s, "group:x")
	d := claim(t, s, "s1", "s1")
	if _, err := s.Reply(ctx, "s1", []int64{m1.ID}, msgstate.Answered, "done", nil); err != nil {
		t.Fatal(err)
	}
	c.add(time.Minute)
	if err := s.Release(ctx, d.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Delivery(ctx, d.ID); got.State != Released || !got.FinishedAt.Equal(c.t) {
		t.Fatalf("delivery %+v", got)
	}
	if got := stateOf(t, s, m2.ID); got != msgstate.NoReply {
		t.Errorf("with-agent message %s", got)
	}
	if got := stateOf(t, s, m1.ID); got != msgstate.Answered {
		t.Errorf("answered message %s", got)
	}
	if err := s.Release(ctx, d.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("released twice: %v", err)
	}
}

// A stuck delivery is usually one that never landed.
func TestReleaseTakesAMessageStillSentToNoReply(t *testing.T) {
	s, _ := newStore(t)
	m := post(t, s, thread(t, s, "group:x", "triage", "s1"), "one")
	d := claim(t, s, "s1", "s1")
	if err := s.Release(ctx, d.ID); err != nil {
		t.Fatal(err)
	}
	if got := stateOf(t, s, m.ID); got != msgstate.NoReply {
		t.Fatalf("state %s", got)
	}
}

func TestMoveDeliveryRequeuesForTheNewSessionInTheirOldOrder(t *testing.T) {
	s, _ := newStore(t)
	th, m1, m2 := sendTwo(t, s, "group:x")
	d := claim(t, s, "s1", "s1")
	m3 := post(t, s, th, "three, queued behind")
	if err := s.MoveDelivery(ctx, d.ID, "s2"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Delivery(ctx, d.ID); got.State != Moved {
		t.Fatalf("delivery %s", got.State)
	}
	if got := thread(t, s, "group:x", "triage", ""); got.Owner != "s2" {
		t.Fatalf("thread owner %q", got.Owner)
	}
	if got := claim(t, s, "s1", "s1", "s2"); got != nil {
		t.Fatalf("the old session got %+v", got)
	}
	d2 := claim(t, s, "s2", "s1", "s2")
	if d2 == nil || !sameIDs(ids(d2.Messages), []int64{m1.ID, m2.ID}) {
		t.Fatalf("delivery %+v", d2)
	}
	d3 := claim(t, s, "s2", "s1", "s2")
	if d3 == nil || !sameIDs(ids(d3.Messages), []int64{m3.ID}) {
		t.Fatalf("delivery %+v", d3)
	}
}

func TestMoveDeliveryLeavesASettledMessageSettled(t *testing.T) {
	s, _ := newStore(t)
	_, m1, m2 := sendTwo(t, s, "group:x")
	d := claim(t, s, "s1", "s1")
	_, _ = s.Reply(ctx, "s1", []int64{m1.ID}, msgstate.Answered, "done", nil)
	if err := s.MoveDelivery(ctx, d.ID, "s2"); err != nil {
		t.Fatal(err)
	}
	if got := stateOf(t, s, m1.ID); got != msgstate.Answered {
		t.Fatalf("m1 %s", got)
	}
	if got := stateOf(t, s, m2.ID); got != msgstate.Sent {
		t.Fatalf("m2 %s", got)
	}
	d2 := claim(t, s, "s2", "s2")
	if d2 == nil || !sameIDs(ids(d2.Messages), []int64{m2.ID}) {
		t.Fatalf("delivery %+v", d2)
	}
}

func TestInterruptSendsInFlightMessagesAgainInTheirOldOrder(t *testing.T) {
	s, _ := newStore(t)
	th, m1, m2 := sendTwo(t, s, "group:x")
	d := claim(t, s, "s1", "s1")
	m3 := post(t, s, th, "three, queued behind")
	got, err := s.Interrupt(ctx)
	if err != nil || len(got) != 1 || got[0].Delivery != d.ID || got[0].Session != "s1" || !sameIDs(got[0].Messages, []int64{m1.ID, m2.ID}) {
		t.Fatalf("interrupted %+v %v", got, err)
	}
	if again, _ := s.Interrupt(ctx); len(again) != 0 {
		t.Fatalf("interrupted again %+v", again)
	}
	if got, _ := s.Delivery(ctx, d.ID); got.State != Stopped {
		t.Fatalf("delivery %s", got.State)
	}
	d2 := claim(t, s, "s1", "s1")
	if d2 == nil || d2.ID == d.ID || !sameIDs(ids(d2.Messages), []int64{m1.ID, m2.ID}) {
		t.Fatalf("delivery %+v", d2)
	}
	if d3 := claim(t, s, "s1", "s1"); d3 == nil || !sameIDs(ids(d3.Messages), []int64{m3.ID}) {
		t.Fatalf("delivery %+v", d3)
	}
}

func TestResendTakesOnlyNoReplyBackToSent(t *testing.T) {
	s, _ := newStore(t)
	_, m1, m2 := sendTwo(t, s, "group:x")
	d := claim(t, s, "s1", "s1")
	_, _ = s.Reply(ctx, "s1", []int64{m1.ID}, msgstate.Answered, "done", nil)
	if err := s.Release(ctx, d.ID); err != nil {
		t.Fatal(err)
	}
	if n, err := s.Resend(ctx, []int64{m1.ID, m2.ID}); err != nil || n != 1 {
		t.Fatalf("resend %d %v", n, err)
	}
	if got := stateOf(t, s, m1.ID); got != msgstate.Answered {
		t.Fatalf("m1 %s", got)
	}
	d2 := claim(t, s, "s1", "s1")
	if d2 == nil || !sameIDs(ids(d2.Messages), []int64{m2.ID}) || d2.BatchID == m2.BatchID {
		t.Fatalf("delivery %+v", d2)
	}
}

// A message sent again goes to whoever has its scope when it is claimed.
func TestResendGoesToTheOwnerNow(t *testing.T) {
	s, _ := newStore(t)
	m := post(t, s, thread(t, s, "group:x", "triage", "s1"), "one")
	d := claim(t, s, "s1", "s1")
	_ = s.Release(ctx, d.ID)
	if n, err := s.Resend(ctx, []int64{m.ID}); err != nil || n != 1 {
		t.Fatalf("resend %d %v", n, err)
	}
	setOwner(t, s, "group:x", "s2")
	if got := claim(t, s, "s2", "s1", "s2"); got == nil || got.Messages[0].ID != m.ID {
		t.Fatalf("delivery %+v", got)
	}
}
