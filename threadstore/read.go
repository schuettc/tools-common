package threadstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"time"
)

// The reads the tools call. Each one names where, at origin, a tool calls what
// it replaces; a read with no call site isn't here.

// Scopes lists every scope with a thread, for a tool whose sessions may claim
// from all of them: sift's ClaimSend and Unsettled take no round (sift
// serve/agent.go agentWait and agentStatus), and casebook hands a session its
// messages whatever the thread (casebook serve/agent_api.go, Queue.Next).
func (s *Store) Scopes(ctx context.Context) ([]string, error) {
	rows, err := s.DB.QueryContext(ctx, "SELECT DISTINCT scope FROM thread_threads ORDER BY scope")
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var scope string
		if err := rows.Scan(&scope); err != nil {
			return nil, err
		}
		out = append(out, scope)
	}
	return out, rows.Err()
}

// SendCount counts a scope's Sends. Sift's review page reads it (sift
// serve/api.go, st.Sends).
func (s *Store) SendCount(ctx context.Context, scope string) (int, error) {
	var n int
	err := s.DB.QueryRowContext(ctx, "SELECT count(*) FROM thread_batches WHERE scope = ? AND state = 'sent'", scope).Scan(&n)
	return n, err
}

// Batch is one Send as a tool's page shows it.
type Batch struct {
	ID     int64
	Scope  string
	SentAt time.Time
	// Messages are the Send's, in order.
	Messages []Message
	// Replies are the agent's rows answering them.
	Replies []Message
	// ClaimedBy is the session the Send is with, or was last with; "" while it
	// waits to be claimed.
	ClaimedBy string
	// Held: the harness holds it, the agent mid-turn.
	Held bool
}

// Batch reads one Send with its messages. A tool writes what its Send says
// (sift's and cull's counts and notes) in its messages' meta, and reads it here
// to word it (sift serve/agent.go and cull serve/agent.go, SendText).
func (s *Store) Batch(ctx context.Context, id int64) (Batch, error) {
	b := Batch{ID: id}
	var sent int64
	err := s.DB.QueryRowContext(ctx, "SELECT scope, sent_at FROM thread_batches WHERE id = ?", id).Scan(&b.Scope, &sent)
	if errors.Is(err, sql.ErrNoRows) {
		return b, ErrNotFound
	}
	if err != nil {
		return b, err
	}
	b.SentAt = tm(sent)
	if b.Messages, err = messages(ctx, s.DB, "batch_id = ? ORDER BY batch_pos, id", id); err != nil {
		return b, err
	}
	if b.Replies, err = messages(ctx, s.DB, "state = '' AND reply_to IN (SELECT id FROM thread_messages WHERE batch_id = ?) ORDER BY id", id); err != nil {
		return b, err
	}
	var waiting bool
	if err := s.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM thread_messages m LEFT JOIN thread_deliveries d ON d.id = m.delivery_id
		WHERE m.batch_id = ? AND `+queued+`)`, id).Scan(&waiting); err != nil {
		return b, err
	}
	if waiting {
		return b, nil
	}
	err = s.DB.QueryRowContext(ctx, "SELECT session, state = 'inflight' AND held != 0 FROM thread_deliveries WHERE batch_id = ? ORDER BY id DESC LIMIT 1", id).Scan(&b.ClaimedBy, &b.Held)
	if errors.Is(err, sql.ErrNoRows) {
		return b, nil
	}
	return b, err
}

// batches reads the Sends a query's ids pick, in its order.
func (s *Store) batches(ctx context.Context, query string, args ...any) ([]Batch, error) {
	rows, err := s.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	_ = rows.Close()
	out := make([]Batch, 0, len(ids))
	for _, id := range ids {
		b, err := s.Batch(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("batch %d: %w", id, err)
		}
		out = append(out, b)
	}
	return out, nil
}

// RecentSends lists a scope's newest n Sends, newest first. The review pages
// show them (sift serve/api.go and cull serve/api.go, st.RecentSends).
func (s *Store) RecentSends(ctx context.Context, scope string, n int) ([]Batch, error) {
	return s.batches(ctx, "SELECT id FROM thread_batches WHERE scope = ? AND state = 'sent' ORDER BY id DESC LIMIT ?", scope, n)
}

// Unsettled lists the Sends in scopes with a message not answered, declined or
// failed, oldest first. The agent's status names them (sift serve/agent.go and
// cull serve/agent.go, st.Unsettled), and casebook reads their meta for what is
// underway (casebook serve/looking.go, Queue.Underway).
func (s *Store) Unsettled(ctx context.Context, scopes []string) ([]Batch, error) {
	may, err := jsonList(scopes)
	if err != nil {
		return nil, err
	}
	return s.batches(ctx, `SELECT b.id FROM thread_batches b WHERE b.state = 'sent'
		AND b.scope IN (SELECT value FROM json_each(?))
		AND EXISTS(SELECT 1 FROM thread_messages m WHERE m.batch_id = b.id AND m.state IN ('sent', 'with_agent', 'working', 'no_reply'))
		ORDER BY b.id`, may)
}

// InFlight lists a session's deliveries in flight, oldest first. casebook's dock
// shows them (casebook serve/page_api.go getSessionDelivery, Queue.Inflight).
func (s *Store) InFlight(ctx context.Context, session string) ([]Delivery, error) {
	rows, err := s.DB.QueryContext(ctx, "SELECT id FROM thread_deliveries WHERE session = ? AND state = 'inflight' ORDER BY id", session)
	if err != nil {
		return nil, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	_ = rows.Close()
	var out []Delivery
	for _, id := range ids {
		d, err := s.Delivery(ctx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

// Pending counts the messages waiting for a session in the scopes it owns.
// casebook's dock shows it (casebook deliver/deliver.go Sessions, the Queued
// column).
func (s *Store) Pending(ctx context.Context, session string) (int, error) {
	var n int
	err := s.DB.QueryRowContext(ctx, `SELECT count(*) FROM thread_messages m
		JOIN thread_threads t ON t.id = m.thread_id
		JOIN thread_scopes o ON o.scope = t.scope
		LEFT JOIN thread_deliveries d ON d.id = m.delivery_id
		WHERE o.owner = ? AND `+queued, session).Scan(&n)
	return n, err
}

// ThreadsByOwner lists the threads of the scopes an owner has, oldest first; ""
// lists all. casebook lists a session's threads (casebook serve/page_api.go
// getThreads, serve/agent_api.go currentThread, serve/apply_api.go
// findOrCreateJobThread, Queue.Threads).
func (s *Store) ThreadsByOwner(ctx context.Context, owner string) ([]Thread, error) {
	if owner == "" {
		return threads(ctx, s.DB, "")
	}
	return threads(ctx, s.DB, "WHERE o.owner = ?", owner)
}

// ThreadByID reads one thread. casebook checks who has a thread before a page's
// message goes into it (casebook serve/page_api.go, Queue.Thread and ownedBy).
func (s *Store) ThreadByID(ctx context.Context, id int64) (Thread, error) {
	th, err := scanThread(s.DB.QueryRowContext(ctx, threadSelect+" WHERE t.id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return th, ErrNotFound
	}
	return th, err
}

// Previous returns the session's most recent finished delivery before id (nil
// if none), which tells the agent what it was working on (casebook
// serve/agent_api.go agentWait, Queue.Previous).
func (s *Store) Previous(ctx context.Context, session string, before int64) (*Delivery, error) {
	var id int64
	err := s.DB.QueryRowContext(ctx, "SELECT id FROM thread_deliveries WHERE session = ? AND id < ? AND state != 'inflight' ORDER BY id DESC LIMIT 1", session, before).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return deliveryRead(ctx, s.DB, s, id)
}

// MessageScopesTx names the scopes of messages, in the tool's own transaction:
// sift moves a round's revision with each reply and report (sift store/loop.go
// ReplySends and ReportSends, bump), and cull tells each project's page (cull
// store/store.go ReplySends and ReportSends, changed).
func (s *Store) MessageScopesTx(ctx context.Context, tx *sql.Tx, ids []int64) ([]string, error) {
	var out []string
	for _, id := range ids {
		var scope string
		err := tx.QueryRowContext(ctx, `SELECT t.scope FROM thread_messages m JOIN thread_threads t ON t.id = m.thread_id WHERE m.id = ?`, id).Scan(&scope)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		if err != nil {
			return nil, err
		}
		if !slices.Contains(out, scope) {
			out = append(out, scope)
		}
	}
	return out, nil
}

// TokenScopeTx names the scope of a session's delivery in flight with token,
// "" if there is none, in the tool's own transaction: a held report moves no
// message but still moves sift's revision (sift store/loop.go ReportSends, bump).
func (s *Store) TokenScopeTx(ctx context.Context, tx *sql.Tx, session, token string) (string, error) {
	id, err := tokenDelivery(ctx, tx, session, token)
	if err != nil || id == 0 {
		return "", err
	}
	var scope string
	err = tx.QueryRowContext(ctx, "SELECT scope FROM thread_deliveries WHERE id = ?", id).Scan(&scope)
	return scope, err
}
