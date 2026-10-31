package threadstore

import (
	"encoding/json"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/subaud-tools/tools-common/msgstate"
)

func claim(t *testing.T, s *Store, session string, present ...string) *Delivery {
	t.Helper()
	d, err := s.Claim(ctx, session, present)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func stateOf(t *testing.T, s *Store, id int64) msgstate.State {
	t.Helper()
	m, err := s.Message(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	return m.State
}

func replies(t *testing.T, s *Store, th Thread) []Message {
	t.Helper()
	all, err := s.Messages(ctx, th.ID)
	if err != nil {
		t.Fatal(err)
	}
	var out []Message
	for _, m := range all {
		if m.State == "" {
			out = append(out, m)
		}
	}
	return out
}

func TestClaimCarriesEverythingQueuedStillSentWithAToken(t *testing.T) {
	s, _ := newStore(t)
	th := thread(t, s, "group:x", "triage", "s1")
	m1, m2 := post(t, s, th, "one"), post(t, s, th, "two")
	d := claim(t, s, "s1", "s1")
	if d == nil || d.Session != "s1" || d.State != InFlight || !sameIDs(ids(d.Messages), []int64{m1.ID, m2.ID}) {
		t.Fatalf("delivery %+v", d)
	}
	if d.Messages[0].State != msgstate.Sent {
		t.Fatalf("state %s, want sent until the harness reports it landed", d.Messages[0].State)
	}
	if !regexp.MustCompile(`^[0-9a-f]{16}$`).MatchString(d.Token) {
		t.Fatalf("token %q", d.Token)
	}
}

func TestClaimKeepsBatchesIntactAndInOrder(t *testing.T) {
	s, c := newStore(t)
	a := thread(t, s, "round:1", "a.md", "s1")
	b := thread(t, s, "round:1", "b.md", "s1")
	first := post(t, s, a, "first")
	c.add(time.Minute)
	d1, d2 := draft(t, s, b, "second"), draft(t, s, a, "third")
	batch, _, _ := s.DraftBatch(ctx, "round:1")
	if err := s.ReorderBatch(ctx, batch, []int64{d2.ID, d1.ID}); err != nil {
		t.Fatal(err)
	}
	c.add(time.Minute)
	last := post(t, s, b, "last")
	c.add(time.Minute)
	if _, err := s.SendBatch(ctx, batch, ""); err != nil {
		t.Fatal(err)
	}
	d := claim(t, s, "s1", "s1")
	if want := []int64{first.ID, last.ID, d2.ID, d1.ID}; d == nil || !sameIDs(ids(d.Messages), want) {
		t.Fatalf("delivery %+v, want %v", d, want)
	}
}

func TestOneInFlightAndMidTurnMessagesWait(t *testing.T) {
	s, c := newStore(t)
	th := thread(t, s, "group:x", "triage", "s1")
	m1 := post(t, s, th, "one")
	d1 := claim(t, s, "s1", "s1")
	c.add(time.Minute)
	m2 := post(t, s, th, "two")
	m3 := post(t, s, th, "three")
	if d := claim(t, s, "s1", "s1"); d != nil {
		t.Fatalf("delivered while one is in flight: %+v", d)
	}
	if _, _, err := s.Reply(ctx, "s1", []int64{m1.ID}, msgstate.Answered, "done", nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Delivery(ctx, d1.ID); got.State != Done {
		t.Fatalf("delivery %s", got.State)
	}
	d2 := claim(t, s, "s1", "s1")
	if d2 == nil || !sameIDs(ids(d2.Messages), []int64{m2.ID, m3.ID}) {
		t.Fatalf("second delivery %+v", d2)
	}
}

func TestADraftIsNotClaimed(t *testing.T) {
	s, _ := newStore(t)
	draft(t, s, thread(t, s, "group:x", "triage", "s1"), "one")
	if d := claim(t, s, "s1", "s1"); d != nil {
		t.Fatalf("delivery %+v", d)
	}
}

func TestAPresentOwnerKeepsItsMessages(t *testing.T) {
	s, _ := newStore(t)
	post(t, s, thread(t, s, "group:x", "triage", "s1"), "one")
	if d := claim(t, s, "s2", "s1", "s2"); d != nil {
		t.Fatalf("s2 took a present owner's message: %+v", d)
	}
	if d := claim(t, s, "s1", "s1", "s2"); d == nil {
		t.Fatal("the owner got nothing")
	}
}

func TestAnotherPresentSessionTakesAGoneOwnersMessages(t *testing.T) {
	s, _ := newStore(t)
	post(t, s, thread(t, s, "group:x", "triage", "s1"), "one")
	d := claim(t, s, "s2", "s2")
	if d == nil || d.Session != "s2" {
		t.Fatalf("delivery %+v", d)
	}
}

func TestAMessageWithNoOwnerGoesToAnySession(t *testing.T) {
	s, _ := newStore(t)
	post(t, s, thread(t, s, "group:x", "triage", ""), "one")
	if d := claim(t, s, "s2", "s1", "s2"); d == nil {
		t.Fatal("no delivery")
	}
}

// With nobody present, every owner is gone, and nil is the same as nobody.
func TestClaimWithNobodyPresent(t *testing.T) {
	s, _ := newStore(t)
	post(t, s, thread(t, s, "group:x", "triage", "s1"), "one")
	d, err := s.Claim(ctx, "s2", nil)
	if err != nil || d == nil {
		t.Fatalf("delivery %+v %v", d, err)
	}
}

// A session that claimed and died before the delivery landed does not strand
// it: once the claimer is gone and PresenceTTL has passed, another takes it,
// the same delivery with a new token.
func TestADeliveryWhoseClaimerIsGoneIsClaimedAgain(t *testing.T) {
	s, c := newStore(t)
	post(t, s, thread(t, s, "group:x", "triage", "s1"), "one")
	first := claim(t, s, "s1", "s1")
	if d := claim(t, s, "s2", "s1", "s2"); d != nil {
		t.Fatalf("s2 took from a present claimer: %+v", d)
	}
	c.add(DefaultPresenceTTL + time.Second)
	d := claim(t, s, "s2", "s2")
	if d == nil || d.ID != first.ID || d.Session != "s2" || d.Token == first.Token {
		t.Fatalf("reclaim %+v", d)
	}
	if _, err := s.Landed(ctx, "s1", first.Token); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Delivery(ctx, first.ID); got.Messages[0].State != msgstate.Sent {
		t.Fatal("the old token still moved the delivery")
	}
}

// A session whose presence snapshot is older than another's fresh claim does
// not take the delivery from it.
func TestAStaleSnapshotDoesNotReclaimAFreshClaim(t *testing.T) {
	s, _ := newStore(t)
	post(t, s, thread(t, s, "group:x", "triage", ""), "one")
	claim(t, s, "s2", "s1", "s2")
	if d := claim(t, s, "s1", "s1"); d != nil {
		t.Fatalf("s1 reclaimed a fresh claim: %+v", d)
	}
}

func TestADeliveryThatLandedIsNeverClaimedAgain(t *testing.T) {
	s, c := newStore(t)
	post(t, s, thread(t, s, "group:x", "triage", "s1"), "one")
	d := claim(t, s, "s1", "s1")
	if _, err := s.Landed(ctx, "s1", d.Token); err != nil {
		t.Fatal(err)
	}
	c.add(DefaultPresenceTTL + time.Second)
	if got := claim(t, s, "s2", "s2"); got != nil {
		t.Fatalf("claimed a delivery with the agent: %+v", got)
	}
}

// The owner still comes first when a delivery is taken back: its claimer went,
// its owner is present.
func TestReclaimLeavesAPresentOwnersDelivery(t *testing.T) {
	s, c := newStore(t)
	post(t, s, thread(t, s, "group:x", "triage", "s1"), "one")
	first := claim(t, s, "s2", "s2") // s1 was gone, s2 took it
	c.add(DefaultPresenceTTL + time.Second)
	if d := claim(t, s, "s3", "s1", "s3"); d != nil {
		t.Fatalf("s3 took a present owner's delivery: %+v", d)
	}
	if d := claim(t, s, "s1", "s1", "s3"); d == nil || d.ID != first.ID {
		t.Fatalf("the owner got %+v", d)
	}
}

func TestHeldMarksOnlyADeliveryThatHasNotLanded(t *testing.T) {
	s, _ := newStore(t)
	post(t, s, thread(t, s, "group:x", "triage", "s1"), "one")
	d := claim(t, s, "s1", "s1")
	if marked, err := s.Held(ctx, "s1", "nope"); err != nil || marked {
		t.Fatalf("an unknown token held it: %v %v", marked, err)
	}
	if marked, err := s.Held(ctx, "s2", d.Token); err != nil || marked {
		t.Fatalf("another session held it: %v %v", marked, err)
	}
	if marked, err := s.Held(ctx, "s1", d.Token); err != nil || !marked {
		t.Fatalf("held %v %v", marked, err)
	}
	if got, _ := s.Delivery(ctx, d.ID); !got.Held {
		t.Fatal("not held")
	}
	if _, err := s.Landed(ctx, "s1", d.Token); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Delivery(ctx, d.ID); got.Held || got.ShownAt.IsZero() {
		t.Fatalf("after landed %+v", got)
	}
	if marked, _ := s.Held(ctx, "s1", d.Token); marked {
		t.Fatal("held after it landed")
	}
}

func TestLandedMovesSentToWithTheAgent(t *testing.T) {
	s, _ := newStore(t)
	m := post(t, s, thread(t, s, "group:x", "triage", "s1"), "one")
	d := claim(t, s, "s1", "s1")
	if moved, err := s.Landed(ctx, "s2", d.Token); err != nil || len(moved) != 0 {
		t.Fatalf("another session's report moved %v %v", moved, err)
	}
	moved, err := s.Landed(ctx, "s1", d.Token)
	if err != nil || !sameIDs(moved, []int64{m.ID}) || stateOf(t, s, m.ID) != msgstate.WithAgent {
		t.Fatalf("moved %v %v state %s", moved, err, stateOf(t, s, m.ID))
	}
}

func TestALateLandedLeavesWorkingWorking(t *testing.T) {
	s, _ := newStore(t)
	m := post(t, s, thread(t, s, "group:x", "triage", "s1"), "one")
	d := claim(t, s, "s1", "s1")
	if _, _, err := s.Reply(ctx, "s1", []int64{m.ID}, msgstate.Working, "on it", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Landed(ctx, "s1", d.Token); err != nil {
		t.Fatal(err)
	}
	if got := stateOf(t, s, m.ID); got != msgstate.Working {
		t.Fatalf("state %s", got)
	}
}

func TestTurnEndedLeavesALandedDeliveryWithNoReply(t *testing.T) {
	s, _ := newStore(t)
	m := post(t, s, thread(t, s, "group:x", "triage", "s1"), "one")
	d := claim(t, s, "s1", "s1")
	if _, err := s.Landed(ctx, "s1", d.Token); err != nil {
		t.Fatal(err)
	}
	ended, err := s.TurnEnded(ctx, "s1")
	if err != nil || ended == nil || ended.ID != d.ID || ended.State != Done {
		t.Fatalf("ended %+v %v", ended, err)
	}
	if got := stateOf(t, s, m.ID); got != msgstate.NoReply {
		t.Fatalf("state %s", got)
	}
}

// A delivery that has not landed stays in flight for the turn it lands in.
func TestTurnEndedLeavesADeliveryThatHasNotLanded(t *testing.T) {
	s, _ := newStore(t)
	m := post(t, s, thread(t, s, "group:x", "triage", "s1"), "one")
	d := claim(t, s, "s1", "s1")
	ended, err := s.TurnEnded(ctx, "s1")
	if err != nil || ended != nil {
		t.Fatalf("ended %+v %v", ended, err)
	}
	if got, _ := s.Delivery(ctx, d.ID); got.State != InFlight || stateOf(t, s, m.ID) != msgstate.Sent {
		t.Fatalf("delivery %+v", got)
	}
}

func TestWorkingSetsTheLineAndAddsNoRow(t *testing.T) {
	s, _ := newStore(t)
	th := thread(t, s, "group:x", "triage", "s1")
	m := post(t, s, th, "one")
	claim(t, s, "s1", "s1")
	for _, line := range []string{"reading", "checking CI"} {
		if _, skipped, err := s.Reply(ctx, "s1", []int64{m.ID}, msgstate.Working, line, nil); err != nil || len(skipped) != 0 {
			t.Fatalf("working: %v %v", skipped, err)
		}
		if got, _ := s.Message(ctx, m.ID); got.State != msgstate.Working || got.Line != line {
			t.Fatalf("message %+v", got)
		}
	}
	if rs := replies(t, s, th); len(rs) != 0 {
		t.Fatalf("rows %+v", rs)
	}
}

func TestASettlingReplyAddsTheAgentsRow(t *testing.T) {
	s, c := newStore(t)
	th := thread(t, s, "group:x", "triage", "s1")
	m := post(t, s, th, "one")
	claim(t, s, "s1", "s1")
	_, _, _ = s.Reply(ctx, "s1", []int64{m.ID}, msgstate.Working, "on it", nil)
	c.add(time.Minute)
	touched, _, err := s.Reply(ctx, "s1", []int64{m.ID}, msgstate.Declined, "won't: Court's call", json.RawMessage(`{"keys":["a"]}`))
	if err != nil || !sameIDs(touched, []int64{m.ID}) {
		t.Fatalf("touched %v %v", touched, err)
	}
	if got, _ := s.Message(ctx, m.ID); got.State != msgstate.Declined || got.Line != "" || !got.SettledAt.Equal(c.t) {
		t.Fatalf("message %+v", got)
	}
	rs := replies(t, s, th)
	if len(rs) != 1 || rs[0].Author != "s1" || rs[0].Body != "won't: Court's call" || rs[0].ReplyTo != m.ID || string(rs[0].Meta) != `{"keys":["a"]}` {
		t.Fatalf("rows %+v", rs)
	}
}

// A send covering several threads is answered once, and each thread shows the
// answer.
func TestASettlingReplyAddsARowInEachThread(t *testing.T) {
	s, _ := newStore(t)
	a := thread(t, s, "round:1", "a.md", "")
	b := thread(t, s, "round:1", "b.md", "")
	d1, d2 := draft(t, s, a, "one"), draft(t, s, b, "two")
	batch, _, _ := s.DraftBatch(ctx, "round:1")
	if _, err := s.SendBatch(ctx, batch, "s1"); err != nil {
		t.Fatal(err)
	}
	claim(t, s, "s1", "s1")
	if _, _, err := s.Reply(ctx, "s1", []int64{d1.ID, d2.ID}, msgstate.Answered, "applied", nil); err != nil {
		t.Fatal(err)
	}
	ra, rb := replies(t, s, a), replies(t, s, b)
	if len(ra) != 1 || ra[0].ReplyTo != d1.ID || len(rb) != 1 || rb[0].ReplyTo != d2.ID {
		t.Fatalf("rows %+v %+v", ra, rb)
	}
}

func TestAReplyToAnotherSessionsMessageRefusesTheWholeCall(t *testing.T) {
	s, _ := newStore(t)
	mine := post(t, s, thread(t, s, "group:x", "mine", "s1"), "one")
	theirs := post(t, s, thread(t, s, "group:x", "theirs", "s2"), "two")
	claim(t, s, "s1", "s1", "s2")
	claim(t, s, "s2", "s1", "s2")
	_, _, err := s.Reply(ctx, "s1", []int64{mine.ID, theirs.ID}, msgstate.Answered, "done", nil)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err %v", err)
	}
	if got := stateOf(t, s, mine.ID); got != msgstate.Sent {
		t.Fatalf("a refused reply moved mine to %s", got)
	}
}

func TestAReplyRefusesAStateThatIsNotAnAcknowledgement(t *testing.T) {
	s, _ := newStore(t)
	m := post(t, s, thread(t, s, "group:x", "triage", "s1"), "one")
	claim(t, s, "s1", "s1")
	for _, st := range []msgstate.State{msgstate.Sent, msgstate.WithAgent, msgstate.NoReply, Draft} {
		if _, _, err := s.Reply(ctx, "s1", []int64{m.ID}, st, "x", nil); err == nil {
			t.Errorf("state %s accepted", st)
		}
	}
}

func TestAnAnswerAfterDeclinedIsSkippedWithNoRow(t *testing.T) {
	s, _ := newStore(t)
	th := thread(t, s, "group:x", "triage", "s1")
	m := post(t, s, th, "one")
	claim(t, s, "s1", "s1")
	_, _, _ = s.Reply(ctx, "s1", []int64{m.ID}, msgstate.Declined, "no", nil)
	touched, skipped, err := s.Reply(ctx, "s1", []int64{m.ID}, msgstate.Answered, "yes after all", nil)
	if err != nil || len(touched) != 0 || len(skipped) != 1 || skipped[0].Reason != "already declined" {
		t.Fatalf("touched %v skipped %+v %v", touched, skipped, err)
	}
	if got := stateOf(t, s, m.ID); got != msgstate.Declined {
		t.Fatalf("state %s", got)
	}
	if rs := replies(t, s, th); len(rs) != 1 {
		t.Fatalf("rows %+v", rs)
	}
}

func TestALateAnswerSettlesNoReplyButWorkingIsSkipped(t *testing.T) {
	s, _ := newStore(t)
	m := post(t, s, thread(t, s, "group:x", "triage", "s1"), "one")
	d := claim(t, s, "s1", "s1")
	_, _ = s.Landed(ctx, "s1", d.Token)
	_, _ = s.TurnEnded(ctx, "s1")
	if _, skipped, err := s.Reply(ctx, "s1", []int64{m.ID}, msgstate.Working, "late", nil); err != nil || len(skipped) != 1 {
		t.Fatalf("working on no_reply: %+v %v", skipped, err)
	}
	if touched, _, err := s.Reply(ctx, "s1", []int64{m.ID}, msgstate.Answered, "late answer", nil); err != nil || len(touched) != 1 {
		t.Fatalf("late answer %v %v", touched, err)
	}
	if got := stateOf(t, s, m.ID); got != msgstate.Answered {
		t.Fatalf("state %s", got)
	}
}

// An answer proves the delivery reached the agent, and it reached it as one event.
func TestAReplyToOneSentMessageBringsTheDeliveryToTheAgent(t *testing.T) {
	s, _ := newStore(t)
	th := thread(t, s, "group:x", "triage", "s1")
	m1, m2 := post(t, s, th, "one"), post(t, s, th, "two")
	d := claim(t, s, "s1", "s1")
	if _, _, err := s.Reply(ctx, "s1", []int64{m1.ID}, msgstate.Answered, "done", nil); err != nil {
		t.Fatal(err)
	}
	if got := stateOf(t, s, m2.ID); got != msgstate.WithAgent {
		t.Fatalf("m2 %s", got)
	}
	if got, _ := s.Delivery(ctx, d.ID); got.ShownAt.IsZero() || got.State != InFlight {
		t.Fatalf("delivery %+v", got)
	}
}
