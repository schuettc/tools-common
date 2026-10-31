package threadstore

// Recovery: what a tool calls when a delivery won't finish on its own. Each of
// these is a takeover, so it asks no msgstate.Allowed. A tool that calls none
// of them is left with Claim's own rule for taking a delivery back from a
// session that has gone.

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

// requeue sends a delivery's unsettled messages again, out of the delivery, for
// owner. Their queue time is kept, so they keep their place in line.
func requeue(ctx context.Context, tx *sql.Tx, delivery int64, owner string) error {
	_, err := tx.ExecContext(ctx, `UPDATE thread_messages SET state = ?, delivery_id = NULL, settled_at = 0, line = '', owner = ?
		WHERE delivery_id = ? AND state NOT IN ('answered', 'declined', 'failed')`, msgstate.Sent, owner, delivery)
	return err
}

// MoveDelivery gives a delivery's unsettled messages to another session: its
// threads move there, with their queued messages, and those messages are sent
// again. An interrupted delivery may move too.
func (s *Store) MoveDelivery(ctx context.Context, id int64, session string) error {
	now := ms(s.Now())
	return s.tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, "UPDATE thread_deliveries SET state = ?, finished_at = ? WHERE id = ? AND state IN ('inflight', 'interrupted')", Moved, now, id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		const threads = "(SELECT thread_id FROM thread_messages WHERE delivery_id = ?)"
		if _, err := tx.ExecContext(ctx, "UPDATE thread_threads SET owner = ? WHERE id IN "+threads, session, id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE thread_messages SET owner = ? WHERE state = 'sent' AND delivery_id IS NULL AND thread_id IN "+threads, session, id); err != nil {
			return err
		}
		return requeue(ctx, tx, id, session)
	})
}

// MoveThread hands a thread to another session. Its queued messages follow.
func (s *Store) MoveThread(ctx context.Context, thread int64, session string) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, "UPDATE thread_threads SET owner = ? WHERE id = ?", session, thread)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		_, err = tx.ExecContext(ctx, "UPDATE thread_messages SET owner = ? WHERE thread_id = ? AND state = 'sent' AND delivery_id IS NULL", session, thread)
		return err
	})
}

// MoveSession moves everything a session has to another at once: its threads,
// what is queued for it and its delivery in flight, whose unsettled messages
// are sent again first in line. It returns how many threads moved and the
// delivery it rescued (0 if none).
func (s *Store) MoveSession(ctx context.Context, from, to string) (threads int, moved int64, err error) {
	now := ms(s.Now())
	err = s.tx(ctx, func(tx *sql.Tx) error {
		var id int64
		err := tx.QueryRowContext(ctx, "SELECT id FROM thread_deliveries WHERE session = ? AND state = 'inflight'", from).Scan(&id)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if id != 0 {
			if _, err := tx.ExecContext(ctx, "UPDATE thread_deliveries SET state = ?, finished_at = ? WHERE id = ?", Moved, now, id); err != nil {
				return err
			}
			if err := requeue(ctx, tx, id, to); err != nil {
				return err
			}
			moved = id
		}
		res, err := tx.ExecContext(ctx, "UPDATE thread_threads SET owner = ? WHERE owner = ?", to, from)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		threads = int(n)
		_, err = tx.ExecContext(ctx, "UPDATE thread_messages SET owner = ? WHERE owner = ? AND state = 'sent' AND delivery_id IS NULL", to, from)
		return err
	})
	if err != nil {
		return 0, 0, err
	}
	return threads, moved, nil
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
// their place in line, for their owners. It returns what it interrupted, so the
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
			if _, err := tx.ExecContext(ctx, `UPDATE thread_messages SET state = ?, delivery_id = NULL, settled_at = 0, line = ''
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

// Resend takes no_reply messages back to sent, queued again for whoever has
// their thread now: the one move msgstate.Allowed draws for them. Any other
// message is left as it is. It returns how many went.
func (s *Store) Resend(ctx context.Context, ids []int64) (int, error) {
	now := ms(s.Now())
	n := 0
	err := s.tx(ctx, func(tx *sql.Tx) error {
		n = 0
		for _, id := range ids {
			var st msgstate.State
			err := tx.QueryRowContext(ctx, "SELECT state FROM thread_messages WHERE id = ?", id).Scan(&st)
			if errors.Is(err, sql.ErrNoRows) || err == nil && !msgstate.Allowed(st, msgstate.Sent) {
				continue
			}
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE thread_messages SET state = ?1, delivery_id = NULL, queued_at = ?2, settled_at = 0, line = '',
				owner = COALESCE((SELECT t.owner FROM thread_threads t WHERE t.id = thread_id AND t.owner != ''), owner)
				WHERE id = ?3`, msgstate.Sent, now, id); err != nil {
				return err
			}
			n++
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return n, nil
}
