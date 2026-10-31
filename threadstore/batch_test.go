package threadstore

import (
	"errors"
	"testing"
	"time"

	"github.com/subaud-tools/tools-common/msgstate"
)

func draft(t *testing.T, s *Store, th Thread, body string) Message {
	t.Helper()
	m, err := s.Draft(ctx, th.ID, NewMessage{Author: "court", Body: body})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func ids(ms []Message) []int64 {
	var out []int64
	for _, m := range ms {
		out = append(out, m.ID)
	}
	return out
}

func sameIDs(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Sift and cull's Send covers a whole round or project, so one batch holds
// the drafts of every thread in a scope.
func TestDraftsInAScopeShareOneBatchAcrossThreads(t *testing.T) {
	s, _ := newStore(t)
	a := thread(t, s, "round:1", "a.md", "")
	b := thread(t, s, "round:1", "b.md", "")
	d1 := draft(t, s, a, "one")
	d2 := draft(t, s, b, "two")
	d3 := draft(t, s, a, "three")
	batch, got, err := s.DraftBatch(ctx, "round:1")
	if err != nil || batch == 0 || !sameIDs(ids(got), []int64{d1.ID, d2.ID, d3.ID}) {
		t.Fatalf("batch %d %+v %v", batch, got, err)
	}
	if got[1].State != Draft || got[1].BatchID != batch || got[1].BatchPos != 2 || !got[1].QueuedAt.IsZero() {
		t.Fatalf("draft %+v", got[1])
	}
}

func TestAnotherScopeHasItsOwnDraftBatch(t *testing.T) {
	s, _ := newStore(t)
	draft(t, s, thread(t, s, "round:1", "a.md", ""), "one")
	if batch, got, err := s.DraftBatch(ctx, "round:2"); err != nil || batch != 0 || got != nil {
		t.Fatalf("batch %d %+v %v", batch, got, err)
	}
	draft(t, s, thread(t, s, "round:2", "a.md", ""), "two")
	one, _, _ := s.DraftBatch(ctx, "round:1")
	two, _, _ := s.DraftBatch(ctx, "round:2")
	if one == two {
		t.Fatalf("both scopes in batch %d", one)
	}
}

func TestEditDraftChangesOnlyADraft(t *testing.T) {
	s, _ := newStore(t)
	th := thread(t, s, "group:x", "triage", "s1")
	d := draft(t, s, th, "one")
	if err := s.EditDraft(ctx, d.ID, "one, edited"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Message(ctx, d.ID); got.Body != "one, edited" {
		t.Fatalf("body %q", got.Body)
	}
	sent := post(t, s, th, "out")
	if err := s.EditDraft(ctx, sent.ID, "changed"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("edit of a sent message: %v", err)
	}
}

func TestRemoveDraftRemovesOnlyADraft(t *testing.T) {
	s, _ := newStore(t)
	th := thread(t, s, "group:x", "triage", "s1")
	d := draft(t, s, th, "one")
	if err := s.RemoveDraft(ctx, d.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Message(ctx, d.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("draft still there: %v", err)
	}
	sent := post(t, s, th, "out")
	if err := s.RemoveDraft(ctx, sent.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("remove of a sent message: %v", err)
	}
}

func TestReorderBatchSetsTheOrder(t *testing.T) {
	s, _ := newStore(t)
	th := thread(t, s, "group:x", "triage", "s1")
	d1, d2 := draft(t, s, th, "one"), draft(t, s, th, "two")
	batch, _, _ := s.DraftBatch(ctx, "group:x")
	if err := s.ReorderBatch(ctx, batch, []int64{d2.ID, d1.ID}); err != nil {
		t.Fatal(err)
	}
	if _, got, _ := s.DraftBatch(ctx, "group:x"); !sameIDs(ids(got), []int64{d2.ID, d1.ID}) {
		t.Fatalf("order %v", ids(got))
	}
}

func TestReorderBatchNeedsEveryDraft(t *testing.T) {
	s, _ := newStore(t)
	th := thread(t, s, "group:x", "triage", "s1")
	d1, d2 := draft(t, s, th, "one"), draft(t, s, th, "two")
	batch, _, _ := s.DraftBatch(ctx, "group:x")
	if err := s.ReorderBatch(ctx, batch, []int64{d2.ID}); err == nil {
		t.Fatal("reordered part of a batch")
	}
	if _, got, _ := s.DraftBatch(ctx, "group:x"); !sameIDs(ids(got), []int64{d1.ID, d2.ID}) {
		t.Fatalf("order %v", ids(got))
	}
}

func TestSendBatchQueuesItAsOneUnit(t *testing.T) {
	s, c := newStore(t)
	a := thread(t, s, "round:1", "a.md", "")
	b := thread(t, s, "round:1", "b.md", "")
	d1, d2 := draft(t, s, a, "one"), draft(t, s, b, "two")
	batch, _, _ := s.DraftBatch(ctx, "round:1")
	c.add(time.Minute)
	if n, err := s.SendBatch(ctx, batch, "s1"); err != nil || n != 2 {
		t.Fatalf("send %d %v", n, err)
	}
	for _, id := range []int64{d1.ID, d2.ID} {
		if got, _ := s.Message(ctx, id); got.State != msgstate.Sent || got.Owner != "s1" || !got.QueuedAt.Equal(c.t) {
			t.Errorf("message %+v", got)
		}
	}
	if _, err := s.SendBatch(ctx, batch, "s1"); err == nil {
		t.Fatal("sent a batch twice")
	}
	// The scope's next draft starts a new batch.
	draft(t, s, a, "three")
	if next, _, _ := s.DraftBatch(ctx, "round:1"); next == batch || next == 0 {
		t.Fatalf("next batch %d", next)
	}
}

// A batch sent with no owner goes to each thread's own.
func TestSendBatchWithNoOwnerUsesTheThreads(t *testing.T) {
	s, _ := newStore(t)
	th := thread(t, s, "group:x", "triage", "s1")
	d := draft(t, s, th, "one")
	batch, _, _ := s.DraftBatch(ctx, "group:x")
	if _, err := s.SendBatch(ctx, batch, ""); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Message(ctx, d.ID); got.Owner != "s1" {
		t.Fatalf("owner %q", got.Owner)
	}
}
