package threadstore

// Recovery: what a tool calls when a delivery won't finish on its own. Each of
// these but Resend is a takeover, so it asks no msgstate.Allowed; the package
// comment lists them. A tool that calls none of them is left with Claim's own
// rule for taking a delivery back from a session that has gone.

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/subaud-tools/tools-common/msgstate"
)

// Delivery states a recovery ends in.
const (
	Released = "released"
	Moved    = "moved"
	Stopped  = "interrupted"
)

// DefaultStuckAfter is when a delivery in flight counts as stuck.
const DefaultStuckAfter = 10 * time.Minute

// stuckAfter returns the effective stuck threshold for this store.
func (s *Store) stuckAfter() time.Duration {
	if s.StuckAfter > 0 {
		return s.StuckAfter
	}
	return DefaultStuckAfter
}

// Stuck lists the deliveries in flight and untouched for StuckAfter, by id,
// with their sessions.
func (s *Store) Stuck(ctx context.Context) (map[int64]string, error) {
	rows, err := s.DB.QueryContext(ctx, "SELECT id, session FROM thread_deliveries WHERE state = 'inflight' AND touched_at < ?", ms(s.Now().Add(-s.stuckAfter())))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[int64]string{}
	for rows.Next() {
		var id int64
		var session string
		if err := rows.Scan(&id, &session); err != nil {
			return nil, err
		}
		out[id] = session
	}
	return out, rows.Err()
}

// Release is Court ending a delivery by hand (a stuck turn): every unsettled
// message, one still sent included (a stuck delivery is usually one that never
// landed), lands on no_reply.
func (s *Store) Release(ctx context.Context, id int64) error {
	now := ms(s.Now())
	return s.tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, "UPDATE thread_deliveries SET state = ?, finished_at = ? WHERE id = ? AND state = 'inflight'", Released, now, id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		_, err = tx.ExecContext(ctx, "UPDATE thread_messages SET state = ?, settled_at = ? WHERE delivery_id = ? AND state IN "+unsettled, msgstate.NoReply, now, id)
		return err
	})
}

// MoveDelivery gives a delivery to another session: its scope moves there, as
// SetOwner moves it, its unsettled messages with it. An interrupted delivery may
// move too. The delivery ends as Moved, unless session already holds it.
func (s *Store) MoveDelivery(ctx context.Context, id int64, session string) error {
	now := ms(s.Now())
	return s.tx(ctx, func(tx *sql.Tx) error {
		var scope, holder, state string
		err := tx.QueryRowContext(ctx, "SELECT scope, session, state FROM thread_deliveries WHERE id = ? AND state IN ('inflight', 'interrupted')", id).Scan(&scope, &holder, &state)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if _, err := s.SetOwnerTx(ctx, tx, scope, session); err != nil {
			return err
		}
		if state == Stopped {
			_, err = tx.ExecContext(ctx, "UPDATE thread_deliveries SET state = ?, finished_at = ? WHERE id = ?", Moved, now, id)
		}
		return err
	})
}

// Interruption is one delivery Interrupt ended: whose it was, and the messages
// in it that were still unsettled (now sent again).
type Interruption struct {
	Delivery int64
	Session  string
	Messages []int64
}

// Interrupt runs at a server's start: deliveries left in flight by the previous
// serve are marked interrupted and their unsettled messages are sent again, in
// their place in line, for their scopes' owners. Each message keeps its delivery
// until another carries it, so an interrupted delivery can still be moved. It
// returns what it interrupted, so the
// server can tell each session about its own deliveries and no others.
func (s *Store) Interrupt(ctx context.Context) ([]Interruption, error) {
	now := ms(s.Now())
	var out []Interruption
	err := s.tx(ctx, func(tx *sql.Tx) error {
		out = nil
		rows, err := tx.QueryContext(ctx, "SELECT id, session FROM thread_deliveries WHERE state = 'inflight' ORDER BY id")
		if err != nil {
			return err
		}
		for rows.Next() {
			var in Interruption
			if err := rows.Scan(&in.Delivery, &in.Session); err != nil {
				_ = rows.Close()
				return err
			}
			out = append(out, in)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return err
		}
		_ = rows.Close()
		for i := range out {
			open, err := messages(ctx, tx, "delivery_id = ? AND state IN "+unsettled+" ORDER BY id", out[i].Delivery)
			if err != nil {
				return err
			}
			for _, m := range open {
				out[i].Messages = append(out[i].Messages, m.ID)
			}
			if _, err := tx.ExecContext(ctx, "UPDATE thread_deliveries SET state = ?, finished_at = ? WHERE id = ?", Stopped, now, out[i].Delivery); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE thread_messages SET state = ?, settled_at = 0, line = ''
				WHERE delivery_id = ? AND state IN `+unsettled, msgstate.Sent, out[i].Delivery); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Resend takes no_reply messages back to sent, the one move msgstate.Allowed
// draws for them: a new Send for each scope among them, to its owner now. Any
// other message is left as it is. It returns how many went.
func (s *Store) Resend(ctx context.Context, ids []int64) (int, error) {
	now := ms(s.Now())
	n := 0
	err := s.tx(ctx, func(tx *sql.Tx) error {
		n = 0
		batches := map[string]int64{}
		for _, id := range ids {
			m, err := message(ctx, tx, id)
			if errors.Is(err, ErrNotFound) || err == nil && !msgstate.Allowed(m.State, msgstate.Sent) {
				continue
			}
			if err != nil {
				return err
			}
			scope, err := scopeOf(ctx, tx, m.ThreadID)
			if err != nil {
				return err
			}
			batch, ok := batches[scope]
			if !ok {
				if batch, err = sentBatch(ctx, tx, scope, now); err != nil {
					return err
				}
				batches[scope] = batch
			}
			n++
			if _, err := tx.ExecContext(ctx, `UPDATE thread_messages SET state = ?, batch_id = ?, batch_pos = ?, delivery_id = NULL, queued_at = ?, settled_at = 0, line = ''
				WHERE id = ?`, msgstate.Sent, batch, n, now, id); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return n, nil
}
