package threadstore

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/subaud-tools/tools-common/msgstate"
)

var errRollback = errors.New("the tool rolled back")

// inToolTx runs fn in a transaction of the tool's own, beside a write of its
// own, the way sift and cull mark decisions sent and record the Send together.
// A short deadline catches a form that reads outside the transaction and waits
// on the database's one connection.
func inToolTx(t *testing.T, s *Store, commit bool, fn func(ctx context.Context, tx *sql.Tx) error) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return s.DB.Tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "INSERT INTO sends(id) VALUES (NULL)"); err != nil {
			return err
		}
		if err := fn(ctx, tx); err != nil {
			return err
		}
		if !commit {
			return errRollback
		}
		return nil
	})
}

func toolRows(t *testing.T, s *Store) int {
	t.Helper()
	var n int
	if err := s.DB.QueryRowContext(ctx, "SELECT count(*) FROM sends").Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// joins runs one form in the tool's transaction twice: rolled back, the store's
// write goes with the tool's, as held tells; committed, both stay.
func joins(t *testing.T, s *Store, fn func(ctx context.Context, tx *sql.Tx) error, held func() bool) {
	t.Helper()
	before := toolRows(t, s)
	if err := inToolTx(t, s, false, fn); !errors.Is(err, errRollback) {
		t.Fatalf("rolled back: %v", err)
	}
	if held() || toolRows(t, s) != before {
		t.Fatal("a write survived the tool's rollback")
	}
	if err := inToolTx(t, s, true, fn); err != nil {
		t.Fatalf("committed: %v", err)
	}
	if !held() || toolRows(t, s) != before+1 {
		t.Fatal("a write was lost with the tool's commit")
	}
}

func TestThreadTxJoinsTheToolsTransaction(t *testing.T) {
	s, _ := newStore(t)
	joins(t, s, func(ctx context.Context, tx *sql.Tx) error {
		th, err := s.ThreadTx(ctx, tx, "round:1", "a.md")
		if err == nil && th.ID == 0 {
			t.Error("no thread")
		}
		return err
	}, func() bool {
		ths, err := s.Threads(ctx, "round:1")
		return err == nil && len(ths) == 1
	})
}

func TestPostTxJoinsTheToolsTransaction(t *testing.T) {
	s, _ := newStore(t)
	th := thread(t, s, "group:x", "triage", "s1")
	joins(t, s, func(ctx context.Context, tx *sql.Tx) error {
		m, err := s.PostTx(ctx, tx, th.ID, NewMessage{Author: "court", Body: "one"})
		if err == nil && m.State != msgstate.Sent {
			t.Errorf("message %+v", m)
		}
		return err
	}, func() bool {
		ms, err := s.Messages(ctx, th.ID)
		return err == nil && len(ms) == 1
	})
}

func TestDraftTxJoinsTheToolsTransaction(t *testing.T) {
	s, _ := newStore(t)
	th := thread(t, s, "round:1", "a.md", "")
	joins(t, s, func(ctx context.Context, tx *sql.Tx) error {
		m, err := s.DraftTx(ctx, tx, th.ID, NewMessage{Author: "court", Body: "one"})
		if err == nil && m.State != Draft {
			t.Errorf("message %+v", m)
		}
		return err
	}, func() bool {
		_, ds, err := s.DraftBatch(ctx, "round:1")
		return err == nil && len(ds) == 1
	})
}

func TestSendBatchTxJoinsTheToolsTransaction(t *testing.T) {
	s, _ := newStore(t)
	d := draft(t, s, thread(t, s, "round:1", "a.md", ""), "one")
	joins(t, s, func(ctx context.Context, tx *sql.Tx) error {
		n, err := s.SendBatchTx(ctx, tx, d.BatchID)
		if err == nil && n != 1 {
			t.Errorf("sent %d", n)
		}
		return err
	}, func() bool { return stateOf(t, s, d.ID) == msgstate.Sent })
}

func TestClaimTxJoinsTheToolsTransaction(t *testing.T) {
	s, _ := newStore(t)
	m := post(t, s, thread(t, s, "group:x", "triage", "s1"), "one")
	joins(t, s, func(ctx context.Context, tx *sql.Tx) error {
		d, err := s.ClaimTx(ctx, tx, "s1", []string{"group:x"}, []string{"s1"})
		if err == nil && (d == nil || !sameIDs(ids(d.Messages), []int64{m.ID})) {
			t.Errorf("delivery %+v", d)
		}
		return err
	}, func() bool {
		got, err := s.Message(ctx, m.ID)
		return err == nil && got.DeliveryID != 0
	})
}

func TestReplyTxJoinsTheToolsTransaction(t *testing.T) {
	s, _ := newStore(t)
	m := post(t, s, thread(t, s, "group:x", "triage", "s1"), "one")
	claim(t, s, "s1", "s1")
	joins(t, s, func(ctx context.Context, tx *sql.Tx) error {
		moved, err := s.ReplyTx(ctx, tx, "s1", []int64{m.ID}, msgstate.Answered, "done", nil)
		if err == nil && !sameIDs(moved, []int64{m.ID}) {
			t.Errorf("moved %v", moved)
		}
		return err
	}, func() bool { return stateOf(t, s, m.ID) == msgstate.Answered })
}

func TestSetOwnerTxJoinsTheToolsTransaction(t *testing.T) {
	s, _ := newStore(t)
	th := thread(t, s, "group:x", "triage", "s1")
	post(t, s, th, "one")
	d := claim(t, s, "s1", "s1")
	joins(t, s, func(ctx context.Context, tx *sql.Tx) error {
		moved, err := s.SetOwnerTx(ctx, tx, "group:x", "s2")
		if err == nil && !sameIDs(moved, []int64{d.ID}) {
			t.Errorf("moved %v", moved)
		}
		return err
	}, func() bool { return thread(t, s, "group:x", "triage", "").Owner == "s2" })
}

func TestHeldTxJoinsTheToolsTransaction(t *testing.T) {
	s, _ := newStore(t)
	post(t, s, thread(t, s, "group:x", "triage", "s1"), "one")
	d := claim(t, s, "s1", "s1")
	joins(t, s, func(ctx context.Context, tx *sql.Tx) error {
		_, err := s.HeldTx(ctx, tx, "s1", d.Token)
		return err
	}, func() bool {
		got, err := s.Delivery(ctx, d.ID)
		return err == nil && got.Held
	})
}

func TestLandedTxJoinsTheToolsTransaction(t *testing.T) {
	s, _ := newStore(t)
	m := post(t, s, thread(t, s, "group:x", "triage", "s1"), "one")
	d := claim(t, s, "s1", "s1")
	joins(t, s, func(ctx context.Context, tx *sql.Tx) error {
		_, err := s.LandedTx(ctx, tx, "s1", d.Token)
		return err
	}, func() bool { return stateOf(t, s, m.ID) == msgstate.WithAgent })
}

func TestTurnEndedTxJoinsTheToolsTransaction(t *testing.T) {
	s, _ := newStore(t)
	m := post(t, s, thread(t, s, "group:x", "triage", "s1"), "one")
	d := claim(t, s, "s1", "s1")
	if _, err := s.Landed(ctx, "s1", d.Token); err != nil {
		t.Fatal(err)
	}
	joins(t, s, func(ctx context.Context, tx *sql.Tx) error {
		ended, err := s.TurnEndedTx(ctx, tx, "s1")
		if err == nil && (len(ended) != 1 || ended[0].State != Done) {
			t.Errorf("ended %+v", ended)
		}
		return err
	}, func() bool { return stateOf(t, s, m.ID) == msgstate.NoReply })
}

// A refused reply returns before any write, so a tool that goes on to commit
// its own transaction has changed nothing of the store's.
func TestARefusedReplyTxWritesNothing(t *testing.T) {
	s, _ := newStore(t)
	th := thread(t, s, "group:x", "triage", "s1")
	settled, open := post(t, s, th, "settled"), post(t, s, th, "open")
	claim(t, s, "s1", "s1")
	claim(t, s, "s1", "s1")
	if _, err := s.Reply(ctx, "s1", []int64{settled.ID}, msgstate.Declined, "no", nil); err != nil {
		t.Fatal(err)
	}
	var no Refused
	err := inToolTx(t, s, true, func(ctx context.Context, tx *sql.Tx) error {
		if _, err := s.ReplyTx(ctx, tx, "s1", []int64{open.ID, settled.ID}, msgstate.Answered, "yes", nil); !errors.As(err, &no) {
			t.Errorf("reply: %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := stateOf(t, s, open.ID); got != msgstate.Sent {
		t.Fatalf("open %s", got)
	}
}
