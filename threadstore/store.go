// Package threadstore is the family's conversation store: threads, messages,
// batches and deliveries with their lifecycle, for a tool whose page talks to
// agent sessions. It is a separate module (its own go.mod and
// threadstore/vX.Y.Z tags) because it needs SQLite, which the root module
// never pulls in. A page that keeps its messages elsewhere still shows them
// through msgstate.Message.
//
// A tool takes Migrations by position into its own database's steps (see
// Migrations) and names each thread's scope: a round, a project, a group or
// whatever else it talks about.
// The store knows nothing of sessions beyond their ids: the tool keeps its
// presence and tells the store who is present.
//
// Every tool follows one delivery rule. A Send is a batch, delivered whole as
// one delivery, and a session can hold several at once. The owner belongs to
// the scope: taking a scope over moves its owner and its unanswered messages
// together, and when the owner has gone another present session that may claim
// the scope takes it. A reply settles all its messages or none.
//
// Every message move goes through msgstate.Allowed or msgstate.Acknowledge,
// except these, which are deliberate:
//   - SendBatch and Post: draft, or nothing, to sent. Draft is before the
//     lifecycle, which starts at sent.
//   - SetOwner and MoveDelivery: with_agent or working back to sent. A takeover
//     says the old session won't finish them, whatever it reached.
//   - Interrupt: with_agent or working back to sent. The serve that handed them
//     out has gone, so nobody knows how far they got.
//   - Release: sent, with_agent or working to no_reply. Court ends a delivery
//     that won't finish, and one that never landed is the usual case.
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
	// PresenceTTL overrides how long Claim leaves a delivery with a claimer that
	// isn't present. Zero means use DefaultPresenceTTL.
	PresenceTTL time.Duration
	// StuckAfter overrides when a delivery in flight counts as stuck. Zero means
	// use DefaultStuckAfter.
	StuckAfter time.Duration
}

// New returns a store over d, whose steps include Migrations.
func New(d *sqlitedb.DB) *Store { return &Store{DB: d, Now: time.Now} }

// The forms named ...Tx do what the plain form does inside the tool's own
// transaction, so a tool can record its decisions and the Send together. The
// plain form is the same call in a transaction of its own. A tool's transaction
// opens with a write, as tx does: one that reads first fails at once if another
// process wrote in between.

// tx runs fn in a transaction whose first statement is a write: a transaction
// that reads first and then writes fails at once if another process wrote in
// between, instead of waiting for it.
func (s *Store) tx(ctx context.Context, fn func(*sql.Tx) error) error {
	return s.DB.Tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "UPDATE thread_threads SET name = name WHERE id = 0"); err != nil {
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
// Owner is its scope's owner, the session its messages go to first; "" when any
// session may take them.
type Thread struct {
	ID        int64
	Scope     string
	Name      string
	Owner     string
	CreatedAt time.Time
}

const threadSelect = `SELECT t.id, t.scope, t.name, COALESCE(o.owner, ''), t.created_at
	FROM thread_threads t LEFT JOIN thread_scopes o ON o.scope = t.scope`

func scanThread(sc interface{ Scan(...any) error }) (Thread, error) {
	var th Thread
	var c int64
	err := sc.Scan(&th.ID, &th.Scope, &th.Name, &th.Owner, &c)
	th.CreatedAt = tm(c)
	return th, err
}

// Thread returns the thread scope and name key, starting it if there is none.
func (s *Store) Thread(ctx context.Context, scope, name string) (Thread, error) {
	var th Thread
	err := s.tx(ctx, func(tx *sql.Tx) error {
		var err error
		th, err = s.ThreadTx(ctx, tx, scope, name)
		return err
	})
	return th, err
}

// ThreadTx is Thread in the tool's own transaction.
func (s *Store) ThreadTx(ctx context.Context, tx *sql.Tx, scope, name string) (Thread, error) {
	var id int64
	err := tx.QueryRowContext(ctx, `INSERT INTO thread_threads(scope, name, created_at) VALUES (?, ?, ?)
		ON CONFLICT(scope, name) DO UPDATE SET scope = excluded.scope RETURNING id`, scope, name, ms(s.Now())).Scan(&id)
	if err != nil {
		return Thread{}, err
	}
	return scanThread(tx.QueryRowContext(ctx, threadSelect+" WHERE t.id = ?", id))
}

// threads lists the threads a where clause picks, oldest first.
func threads(ctx context.Context, q querier, where string, args ...any) ([]Thread, error) {
	rows, err := q.QueryContext(ctx, threadSelect+" "+where+" ORDER BY t.id", args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Thread
	for rows.Next() {
		th, err := scanThread(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, th)
	}
	return out, rows.Err()
}

// Threads lists a scope's threads, oldest first.
func (s *Store) Threads(ctx context.Context, scope string) ([]Thread, error) {
	return threads(ctx, s.DB, "WHERE t.scope = ?", scope)
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
	// DeliveryID is the delivery that last carried it, as casebook's
	// deliver.Message says on its wire. It stays when the message is queued
	// again, so a delivery that was interrupted or moved still knows what it
	// held, until another delivery carries it.
	DeliveryID int64
	State      msgstate.State
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

const msgCols = "id, thread_id, author, body, meta, COALESCE(reply_to, 0), COALESCE(batch_id, 0), batch_pos, COALESCE(delivery_id, 0), state, line, created_at, queued_at, settled_at"

func scanMessage(sc interface{ Scan(...any) error }) (Message, error) {
	var m Message
	var meta string
	var c, qd, st int64
	if err := sc.Scan(&m.ID, &m.ThreadID, &m.Author, &m.Body, &meta, &m.ReplyTo, &m.BatchID, &m.BatchPos, &m.DeliveryID, &m.State, &m.Line, &c, &qd, &st); err != nil {
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
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
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
	return message(ctx, s.DB, id)
}

func message(ctx context.Context, q querier, id int64) (Message, error) {
	m, err := scanMessage(q.QueryRowContext(ctx, "SELECT "+msgCols+" FROM thread_messages WHERE id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return m, ErrNotFound
	}
	return m, err
}

// row places a message being written.
type row struct {
	state       msgstate.State
	batch       int64
	pos         int
	now, queued int64
}

// insert adds a message.
func insert(ctx context.Context, tx *sql.Tx, thread int64, m NewMessage, r row) (int64, error) {
	if strings.TrimSpace(m.Body) == "" {
		return 0, fmt.Errorf("empty message")
	}
	var reply, batch sql.NullInt64
	if m.ReplyTo != 0 {
		reply = sql.NullInt64{Int64: m.ReplyTo, Valid: true}
	}
	if r.batch != 0 {
		batch = sql.NullInt64{Int64: r.batch, Valid: true}
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO thread_messages(thread_id, author, body, meta, reply_to, batch_id, batch_pos, state, created_at, queued_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, thread, m.Author, m.Body, string(m.Meta), reply, batch, r.pos, r.state, r.now, r.queued)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// scopeOf is a thread's scope.
func scopeOf(ctx context.Context, q querier, thread int64) (string, error) {
	var scope string
	err := q.QueryRowContext(ctx, "SELECT scope FROM thread_threads WHERE id = ?", thread).Scan(&scope)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return scope, err
}

// sentBatch starts a batch already sent: a Send that had no drafts.
func sentBatch(ctx context.Context, tx *sql.Tx, scope string, now int64) (int64, error) {
	res, err := tx.ExecContext(ctx, "INSERT INTO thread_batches(scope, state, created_at, sent_at) VALUES (?, 'sent', ?, ?)", scope, now, now)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// Post adds a message to a thread and sends it at once: a Send of one, queued
// for the thread's scope.
func (s *Store) Post(ctx context.Context, thread int64, m NewMessage) (Message, error) {
	var out Message
	err := s.tx(ctx, func(tx *sql.Tx) error {
		var err error
		out, err = s.PostTx(ctx, tx, thread, m)
		return err
	})
	return out, err
}

// PostTx is Post in the tool's own transaction.
func (s *Store) PostTx(ctx context.Context, tx *sql.Tx, thread int64, m NewMessage) (Message, error) {
	now := ms(s.Now())
	scope, err := scopeOf(ctx, tx, thread)
	if err != nil {
		return Message{}, err
	}
	batch, err := sentBatch(ctx, tx, scope, now)
	if err != nil {
		return Message{}, err
	}
	id, err := insert(ctx, tx, thread, m, row{state: msgstate.Sent, batch: batch, pos: 1, now: now, queued: now})
	if err != nil {
		return Message{}, err
	}
	return message(ctx, tx, id)
}

// Note adds a row outside the lifecycle: no state, never delivered. A tool
// uses it for what it writes itself, such as a turn's progress summary.
func (s *Store) Note(ctx context.Context, thread int64, m NewMessage) (Message, error) {
	var out Message
	err := s.tx(ctx, func(tx *sql.Tx) error {
		id, err := insert(ctx, tx, thread, m, row{now: ms(s.Now())})
		if err != nil {
			return err
		}
		out, err = message(ctx, tx, id)
		return err
	})
	return out, err
}
