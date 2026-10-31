// Package threadstore is the family's conversation store: threads, messages,
// batches and deliveries with their lifecycle, for a tool whose page talks to
// agent sessions. It is a separate module (its own go.mod and
// threadstore/vX.Y.Z tags) because it needs SQLite, which the root module
// never pulls in. A page that keeps its messages elsewhere still shows them
// through msgstate.Message.
//
// A tool appends Migrations to its own database's steps and names each
// thread's scope: a round, a project, a group or whatever else it talks about.
// The store knows nothing of sessions beyond their ids: the tool keeps its
// presence and tells the store who is present.
package threadstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/subaud-tools/tools-common/msgstate"
	"github.com/subaud-tools/tools-common/sqlitedb"
)

// ErrNotFound means an id doesn't exist (or isn't the caller's).
var ErrNotFound = errors.New("not found")

// Store is the conversation store over a tool's database.
type Store struct {
	DB  *sqlitedb.DB
	Now func() time.Time
}

// New returns a store over d, whose steps include Migrations.
func New(d *sqlitedb.DB) *Store { return &Store{DB: d, Now: time.Now} }

// tx runs fn in a transaction whose first statement is a write: a transaction
// that reads first and then writes fails at once if another process wrote in
// between, instead of waiting for it.
func (s *Store) tx(ctx context.Context, fn func(*sql.Tx) error) error {
	return s.DB.Tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "UPDATE thread_threads SET owner = owner WHERE id = 0"); err != nil {
			return err
		}
		return fn(tx)
	})
}

func ms(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixMilli()
}

func tm(v int64) time.Time {
	if v == 0 {
		return time.Time{}
	}
	return time.UnixMilli(v).UTC()
}

// Thread is one conversation, keyed by the scope the tool names and a name.
// Owner is the session its messages go to first; "" when any session may take
// them.
type Thread struct {
	ID        int64
	Scope     string
	Name      string
	Owner     string
	CreatedAt time.Time
}

// Thread returns the thread scope and name key, starting it with owner if
// there is none. A thread that exists keeps its owner (MoveThread changes it).
func (s *Store) Thread(ctx context.Context, scope, name, owner string) (Thread, error) {
	var th Thread
	var c int64
	err := s.DB.QueryRowContext(ctx, `INSERT INTO thread_threads(scope, name, owner, created_at) VALUES (?, ?, ?, ?)
		ON CONFLICT(scope, name) DO UPDATE SET scope = excluded.scope
		RETURNING id, scope, name, owner, created_at`, scope, name, owner, ms(s.Now())).
		Scan(&th.ID, &th.Scope, &th.Name, &th.Owner, &c)
	th.CreatedAt = tm(c)
	return th, err
}

// Threads lists a scope's threads, oldest first.
func (s *Store) Threads(ctx context.Context, scope string) ([]Thread, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, scope, name, owner, created_at FROM thread_threads WHERE scope = ? ORDER BY id`, scope)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Thread
	for rows.Next() {
		var th Thread
		var c int64
		if err := rows.Scan(&th.ID, &th.Scope, &th.Name, &th.Owner, &c); err != nil {
			return nil, err
		}
		th.CreatedAt = tm(c)
		out = append(out, th)
	}
	return out, rows.Err()
}

// Draft is a message's state in a batch not sent yet, before the lifecycle.
const Draft msgstate.State = "draft"

// Message is one message in a thread. State is Draft, one of msgstate's states
// or, for a row outside the lifecycle (the agent's reply, a tool's note), "".
type Message struct {
	ID       int64
	ThreadID int64
	Author   string
	Body     string
	// Meta is the tool's own JSON (attachments, a progress summary); the store
	// only keeps it.
	Meta    json.RawMessage
	ReplyTo int64
	// BatchID and BatchPos place a message in the batch it went out in.
	BatchID  int64
	BatchPos int
	// Owner is the session the message goes to first, recorded when it was
	// queued.
	Owner string
	State msgstate.State
	// Line is the agent's one line on what it is doing, while the message is
	// working; "" otherwise.
	Line      string
	CreatedAt time.Time
	QueuedAt  time.Time
	SettledAt time.Time
}

// NewMessage is what a caller writes of a message.
type NewMessage struct {
	Author  string
	Body    string
	Meta    json.RawMessage
	ReplyTo int64
}

const msgCols = "id, thread_id, author, body, meta, COALESCE(reply_to, 0), COALESCE(batch_id, 0), batch_pos, owner, state, line, created_at, queued_at, settled_at"

func scanMessage(sc interface{ Scan(...any) error }) (Message, error) {
	var m Message
	var meta string
	var c, qd, st int64
	if err := sc.Scan(&m.ID, &m.ThreadID, &m.Author, &m.Body, &meta, &m.ReplyTo, &m.BatchID, &m.BatchPos, &m.Owner, &m.State, &m.Line, &c, &qd, &st); err != nil {
		return m, err
	}
	if meta != "" {
		m.Meta = json.RawMessage(meta)
	}
	m.CreatedAt, m.QueuedAt, m.SettledAt = tm(c), tm(qd), tm(st)
	return m, nil
}

// querier is what the store reads through: the database or a transaction.
type querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

func messages(ctx context.Context, q querier, where string, args ...any) ([]Message, error) {
	rows, err := q.QueryContext(ctx, "SELECT "+msgCols+" FROM thread_messages WHERE "+where, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Message
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// Messages lists a thread's messages in order.
func (s *Store) Messages(ctx context.Context, thread int64) ([]Message, error) {
	return messages(ctx, s.DB, "thread_id = ? ORDER BY id", thread)
}

// Message reads one message.
func (s *Store) Message(ctx context.Context, id int64) (Message, error) {
	m, err := scanMessage(s.DB.QueryRowContext(ctx, "SELECT "+msgCols+" FROM thread_messages WHERE id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return m, ErrNotFound
	}
	return m, err
}

// insert adds a message with the given state, owner and queue time.
func insert(ctx context.Context, x interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}, thread int64, m NewMessage, owner string, state msgstate.State, now, queued int64) (int64, error) {
	if strings.TrimSpace(m.Body) == "" {
		return 0, fmt.Errorf("empty message")
	}
	var reply sql.NullInt64
	if m.ReplyTo != 0 {
		reply = sql.NullInt64{Int64: m.ReplyTo, Valid: true}
	}
	res, err := x.ExecContext(ctx, `INSERT INTO thread_messages(thread_id, author, body, meta, reply_to, owner, state, created_at, queued_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, thread, m.Author, m.Body, string(m.Meta), reply, owner, state, now, queued)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// Post adds a message to a thread: sent, queued for its thread's owner.
func (s *Store) Post(ctx context.Context, thread int64, m NewMessage) (Message, error) {
	now := ms(s.Now())
	var id int64
	err := s.tx(ctx, func(tx *sql.Tx) error {
		var owner string
		if err := tx.QueryRowContext(ctx, "SELECT owner FROM thread_threads WHERE id = ?", thread).Scan(&owner); errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		var err error
		id, err = insert(ctx, tx, thread, m, owner, msgstate.Sent, now, now)
		return err
	})
	if err != nil {
		return Message{}, err
	}
	return s.Message(ctx, id)
}

// Note adds a row outside the lifecycle: no state, never delivered. A tool
// uses it for what it writes itself, such as a turn's progress summary.
func (s *Store) Note(ctx context.Context, thread int64, m NewMessage) (Message, error) {
	id, err := insert(ctx, s.DB, thread, m, "", "", ms(s.Now()), 0)
	if err != nil {
		return Message{}, err
	}
	return s.Message(ctx, id)
}
