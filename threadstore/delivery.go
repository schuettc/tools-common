package threadstore

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/subaud-tools/tools-common/msgstate"
)

// Delivery states.
const (
	InFlight = "inflight"
	Done     = "done"
)

// DefaultPresenceTTL is how long after a claim another session may take it
// from a claimer that isn't present: a session's check-ins are that far apart,
// so a claim younger than this may just not be in the caller's snapshot yet.
const DefaultPresenceTTL = 90 * time.Second

// unsettled is the SQL list of message states still open in a delivery.
const unsettled = "('sent', 'with_agent', 'working')"

// Delivery is a set of messages that went to a session together. A session has
// at most one in flight. It ends when the agent settles every message in it,
// when the harness reports the turn ended after it landed, or by recovery.
type Delivery struct {
	ID         int64
	Session    string
	State      string
	SentAt     time.Time
	TouchedAt  time.Time
	FinishedAt time.Time
	// ShownAt is when it reached the agent; zero until then.
	ShownAt time.Time
	// Held: the harness reported it held, the agent mid-turn, and it has not
	// landed since.
	Held     bool
	Messages []Message
	// Token is the delivery's report token: the harness names it when it
	// reports the delivery held or landed. It changes when the delivery is
	// claimed again, so a report from the old claimer does nothing.
	Token string
}

// presenceTTL returns the effective reclaim threshold for this store.
func (s *Store) presenceTTL() time.Duration {
	if s.PresenceTTL > 0 {
		return s.PresenceTTL
	}
	return DefaultPresenceTTL
}

// Claim hands session what it may take, when it has nothing in flight: first a
// delivery taken back from a session that has gone, otherwise a new delivery of
// every queued message it may take, in order, batches intact. The messages stay
// sent: the harness's landed report moves them. It returns nil when there is
// nothing, or while session has a delivery in flight.
//
// Session may take a message whose owner is session, or is "", or isn't in
// present, the sessions the tool sees right now. A tool that never wants a
// message to pass to another session names every session it knows as present.
//
// A delivery is taken back while it has not landed, its claimer isn't in
// present, nobody has touched it for PresenceTTL, and session may take all its
// messages: a session that died before its delivery landed does not strand it,
// and a caller whose present was read before another session claimed does not
// take the delivery from it. A delivery that reached the agent is never handed
// out again.
func (s *Store) Claim(ctx context.Context, session string, present []string) (*Delivery, error) {
	if present == nil {
		present = []string{} // json null would match nobody's NOT IN
	}
	gone, err := json.Marshal(present)
	if err != nil {
		return nil, err
	}
	now := s.Now()
	var id int64
	err = s.tx(ctx, func(tx *sql.Tx) error {
		var busy bool
		if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM thread_deliveries WHERE session = ? AND state = 'inflight')", session).Scan(&busy); err != nil {
			return err
		}
		if busy {
			return nil
		}
		err := tx.QueryRowContext(ctx, `SELECT d.id FROM thread_deliveries d
			WHERE d.state = 'inflight' AND d.shown_at = 0 AND d.touched_at <= ?1
			AND d.session NOT IN (SELECT value FROM json_each(?2))
			AND NOT EXISTS(SELECT 1 FROM thread_messages m WHERE m.delivery_id = d.id
				AND m.owner NOT IN ('', ?3) AND m.owner IN (SELECT value FROM json_each(?2)))
			ORDER BY d.id LIMIT 1`, ms(now.Add(-s.presenceTTL())), string(gone), session).Scan(&id)
		if err == nil {
			_, err = tx.ExecContext(ctx, "UPDATE thread_deliveries SET session = ?, token = ?, touched_at = ?, held = 0 WHERE id = ?", session, reportToken(), ms(now), id)
			return err
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		const mayTake = `state = 'sent' AND delivery_id IS NULL AND (owner IN ('', ?1) OR owner NOT IN (SELECT value FROM json_each(?2)))`
		var n int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM thread_messages WHERE "+mayTake, session, string(gone)).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			return nil
		}
		res, err := tx.ExecContext(ctx, "INSERT INTO thread_deliveries(session, state, sent_at, touched_at, token) VALUES (?, 'inflight', ?, ?, ?)", session, ms(now), ms(now), reportToken())
		if err != nil {
			return err
		}
		if id, err = res.LastInsertId(); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "UPDATE thread_messages SET delivery_id = ?3 WHERE "+mayTake, session, string(gone), id)
		return err
	})
	if err != nil || id == 0 {
		return nil, err
	}
	d, err := s.Delivery(ctx, id)
	return &d, err
}

// reportToken is a delivery's report token: 16 random hex characters, which
// the Claude Code mod passes back as an argument, so alphanumeric.
func reportToken() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b) // crypto/rand.Read never fails
	return hex.EncodeToString(b)
}

// Delivery reads one delivery with its messages.
func (s *Store) Delivery(ctx context.Context, id int64) (Delivery, error) {
	var d Delivery
	var sent, touched, fin, shown int64
	err := s.DB.QueryRowContext(ctx, "SELECT id, session, state, sent_at, touched_at, finished_at, shown_at, held != 0, token FROM thread_deliveries WHERE id = ?", id).
		Scan(&d.ID, &d.Session, &d.State, &sent, &touched, &fin, &shown, &d.Held, &d.Token)
	if errors.Is(err, sql.ErrNoRows) {
		return d, ErrNotFound
	}
	if err != nil {
		return d, err
	}
	d.SentAt, d.TouchedAt, d.FinishedAt, d.ShownAt = tm(sent), tm(touched), tm(fin), tm(shown)
	d.Messages, err = messages(ctx, s.DB, "delivery_id = ? ORDER BY queued_at, COALESCE(batch_id, 0), batch_pos, id", id)
	return d, err
}

// tokenDelivery is the session's delivery in flight with token, or 0.
func tokenDelivery(ctx context.Context, tx *sql.Tx, session, token string) (int64, error) {
	if token == "" {
		return 0, nil
	}
	var id int64
	err := tx.QueryRowContext(ctx, "SELECT id FROM thread_deliveries WHERE session = ? AND token = ? AND state = 'inflight'", session, token).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return id, err
}

// Held is the harness reporting that the session's delivery with token is held,
// the agent mid-turn. It marks the delivery held while it has not landed. An
// unknown token, or another session's, does nothing. It reports whether it
// marked one.
func (s *Store) Held(ctx context.Context, session, token string) (bool, error) {
	var marked bool
	err := s.tx(ctx, func(tx *sql.Tx) error {
		id, err := tokenDelivery(ctx, tx, session, token)
		if err != nil || id == 0 {
			return err
		}
		res, err := tx.ExecContext(ctx, "UPDATE thread_deliveries SET held = 1 WHERE id = ? AND shown_at = 0 AND held = 0", id)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		marked = n > 0
		return nil
	})
	return marked, err
}

// Landed is the harness reporting that the session's delivery with token landed
// in the agent's turn: its sent messages are with the agent, it is no longer
// held, and it is shown. An unknown token, or another session's, does nothing.
// It returns the ids it moved.
func (s *Store) Landed(ctx context.Context, session, token string) ([]int64, error) {
	now := ms(s.Now())
	var moved []int64
	err := s.tx(ctx, func(tx *sql.Tx) error {
		id, err := tokenDelivery(ctx, tx, session, token)
		if err != nil || id == 0 {
			return err
		}
		moved, err = landed(ctx, tx, now, id)
		return err
	})
	return moved, err
}

// landed is Landed's core on delivery id, shared with Reply.
func landed(ctx context.Context, tx *sql.Tx, now, id int64) ([]int64, error) {
	moved, err := move(ctx, tx, now, id, msgstate.WithAgent)
	if err != nil {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, "UPDATE thread_deliveries SET held = 0, touched_at = ?1, shown_at = CASE WHEN shown_at = 0 THEN ?1 ELSE shown_at END WHERE id = ?2", now, id)
	return moved, err
}

// move moves each message in delivery to to, when msgstate.Allowed draws the
// move from its state; a refused move is skipped, so a late landed report
// leaves a working message working. It returns the ids it moved.
func move(ctx context.Context, tx *sql.Tx, now, delivery int64, to msgstate.State) ([]int64, error) {
	rows, err := tx.QueryContext(ctx, "SELECT id, state FROM thread_messages WHERE delivery_id = ? ORDER BY id", delivery)
	if err != nil {
		return nil, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		var st msgstate.State
		if err := rows.Scan(&id, &st); err != nil {
			_ = rows.Close()
			return nil, err
		}
		if msgstate.Allowed(st, to) {
			ids = append(ids, id)
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	_ = rows.Close()
	settled := int64(0)
	if to == msgstate.NoReply {
		settled = now
	}
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx, "UPDATE thread_messages SET state = ?, settled_at = ? WHERE id = ?", to, settled, id); err != nil {
			return nil, err
		}
	}
	return ids, nil
}

// TurnEnded is the harness reporting that the session's turn ended. The
// session's delivery in flight ends only when it has landed: its with_agent and
// working messages are no_reply. A delivery that has not landed stays in flight
// for the turn it lands in. It returns the ended delivery, or nil.
func (s *Store) TurnEnded(ctx context.Context, session string) (*Delivery, error) {
	now := ms(s.Now())
	var ended int64
	err := s.tx(ctx, func(tx *sql.Tx) error {
		var id, shownAt int64
		err := tx.QueryRowContext(ctx, "SELECT id, shown_at FROM thread_deliveries WHERE session = ? AND state = 'inflight'", session).Scan(&id, &shownAt)
		if errors.Is(err, sql.ErrNoRows) || err == nil && shownAt == 0 {
			return nil // nothing in flight, or it has not landed
		}
		if err != nil {
			return err
		}
		if _, err := move(ctx, tx, now, id, msgstate.NoReply); err != nil {
			return err
		}
		ended = id
		_, err = tx.ExecContext(ctx, "UPDATE thread_deliveries SET state = ?, finished_at = ? WHERE id = ?", Done, now, id)
		return err
	})
	if err != nil || ended == 0 {
		return nil, err
	}
	d, err := s.Delivery(ctx, ended)
	return &d, err
}

// SkippedMessage reports why one id was not changed by Reply.
type SkippedMessage struct {
	ID     int64
	State  msgstate.State
	Reason string
}

// Reply is the agent acknowledging messages from its deliveries: working (text
// is the line under the message, and no thread row), answered, declined or
// failed (text is the agent's reply, added to each thread among the ids,
// answering the first id in it, with meta). Each id moves when
// msgstate.Acknowledge draws the move; one already settled, or a move it
// refuses, is skipped with why. An id that isn't in one of session's deliveries
// refuses the whole call. A reply to a message still sent brings its whole
// delivery to the agent (it reached it as one event). A delivery ends once
// every message in it is settled. It returns the ids it moved.
func (s *Store) Reply(ctx context.Context, session string, ids []int64, state msgstate.State, text string, meta json.RawMessage) ([]int64, []SkippedMessage, error) {
	if state != msgstate.Working && !msgstate.Final(state) {
		return nil, nil, fmt.Errorf("state %q: want working, answered, declined or failed", state)
	}
	if len(ids) == 0 {
		return nil, nil, fmt.Errorf("no message ids")
	}
	now := ms(s.Now())
	var touched []int64
	var skipped []SkippedMessage
	err := s.tx(ctx, func(tx *sql.Tx) error {
		deliveries := map[int64]bool{}
		var threads []int64
		answers := map[int64]int64{} // thread: the first id moved in it
		for _, id := range ids {
			var did, thread int64
			var st msgstate.State
			err := tx.QueryRowContext(ctx, `SELECT m.delivery_id, m.thread_id, m.state FROM thread_messages m
				JOIN thread_deliveries d ON d.id = m.delivery_id WHERE m.id = ? AND d.session = ?`, id, session).Scan(&did, &thread, &st)
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("message %d: %w (not delivered to %s)", id, ErrNotFound, session)
			}
			if err != nil {
				return err
			}
			switch {
			case msgstate.Final(st):
				skipped = append(skipped, SkippedMessage{ID: id, State: st, Reason: "already " + string(st)})
				continue
			case st == msgstate.Working && state == msgstate.Working:
				// Only the line changes: not a move, so nothing to ask.
			case !msgstate.Acknowledge(st, state):
				skipped = append(skipped, SkippedMessage{ID: id, State: st, Reason: "not settleable with state " + string(state)})
				continue
			}
			if st == msgstate.Sent {
				if _, err := landed(ctx, tx, now, did); err != nil {
					return err
				}
			}
			line, settled := text, int64(0)
			if msgstate.Final(state) {
				line, settled = "", now
			}
			if _, err := tx.ExecContext(ctx, "UPDATE thread_messages SET state = ?, line = ?, settled_at = ? WHERE id = ?", state, line, settled, id); err != nil {
				return err
			}
			touched = append(touched, id)
			if _, ok := answers[thread]; !ok {
				answers[thread] = id
				threads = append(threads, thread)
			}
			if st != msgstate.NoReply {
				// A late reply leaves its ended delivery alone.
				deliveries[did] = true
			}
		}
		if text != "" && msgstate.Final(state) {
			for _, thread := range threads {
				if _, err := insert(ctx, tx, thread, NewMessage{Author: session, Body: text, Meta: meta, ReplyTo: answers[thread]}, "", "", now, 0); err != nil {
					return err
				}
			}
		}
		for did := range deliveries {
			if _, err := tx.ExecContext(ctx, "UPDATE thread_deliveries SET touched_at = ?1, shown_at = CASE WHEN shown_at = 0 THEN ?1 ELSE shown_at END WHERE id = ?2", now, did); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE thread_deliveries SET state = ?1, finished_at = ?2 WHERE id = ?3 AND state = 'inflight'
				AND NOT EXISTS(SELECT 1 FROM thread_messages WHERE delivery_id = ?3 AND state IN `+unsettled+`)`, Done, now, did); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return touched, skipped, nil
}
