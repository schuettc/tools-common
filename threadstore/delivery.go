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

// DefaultPresenceTTL is how long the store waits before believing a session is
// gone: a session's check-ins are that far apart, so a claimer or a new owner
// younger than this may just not be in the caller's snapshot yet.
const DefaultPresenceTTL = 90 * time.Second

// unsettled is the SQL list of message states still open in a delivery.
const unsettled = "('sent', 'with_agent', 'working')"

// queued is the SQL test that message m waits for a delivery: sent, and carried
// by no delivery in flight. It joins the delivery as d.
const queued = "m.state = 'sent' AND (d.id IS NULL OR d.state != 'inflight')"

// Delivery is one Send on its way to a session: the messages of one batch still
// open when it was claimed. A session can hold several. It ends when the agent
// settles every message in it, when the harness reports the turn ended after it
// landed, or by recovery.
type Delivery struct {
	ID         int64
	Scope      string
	BatchID    int64
	Session    string
	State      string
	SentAt     time.Time
	TouchedAt  time.Time
	FinishedAt time.Time
	// ShownAt is when it reached the agent; zero until then.
	ShownAt time.Time
	// Stuck: in flight and untouched for StuckAfter.
	Stuck bool
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

// SetOwner makes session the owner of scope and brings it what the scope has
// not answered, in one transaction. What is queued simply goes to the new owner.
// A message another session holds or left unanswered (still sent, with the
// agent, working or no reply) is queued again with its place in line, and each
// delivery in flight with another session ends as Moved. It returns those
// deliveries. What session already holds stays with it, so naming the same owner
// again moves nothing.
func (s *Store) SetOwner(ctx context.Context, scope, session string) ([]int64, error) {
	var moved []int64
	err := s.tx(ctx, func(tx *sql.Tx) error {
		var err error
		moved, err = s.SetOwnerTx(ctx, tx, scope, session)
		return err
	})
	return moved, err
}

// SetOwnerTx is SetOwner in the tool's own transaction.
func (s *Store) SetOwnerTx(ctx context.Context, tx *sql.Tx, scope, session string) ([]int64, error) {
	now := ms(s.Now())
	if _, err := tx.ExecContext(ctx, `INSERT INTO thread_scopes(scope, owner, owned_at) VALUES (?1, ?2, ?3)
		ON CONFLICT(scope) DO UPDATE SET owner = ?2, owned_at = ?3`, scope, session, now); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, "SELECT id FROM thread_deliveries WHERE scope = ? AND state = 'inflight' AND session != ? ORDER BY id", scope, session)
	if err != nil {
		return nil, err
	}
	var moved []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return nil, err
		}
		moved = append(moved, id)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	_ = rows.Close()
	for _, id := range moved {
		if _, err := tx.ExecContext(ctx, "UPDATE thread_deliveries SET state = ?, finished_at = ? WHERE id = ?", Moved, now, id); err != nil {
			return nil, err
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE thread_messages SET state = ?, settled_at = 0, line = ''
		WHERE state IN ('sent', 'with_agent', 'working', 'no_reply')
		AND thread_id IN (SELECT id FROM thread_threads WHERE scope = ?)
		AND delivery_id IN (SELECT id FROM thread_deliveries WHERE session != ?)`, msgstate.Sent, scope, session)
	return moved, err
}

// Claim hands session one Send from scopes, the scopes it may claim: first from
// the ones it owns, then from those with no owner or whose owner has gone. In
// each, it first takes back a delivery from a session that has gone, otherwise
// the oldest Send waiting, whole. The messages stay sent: the harness's landed
// report moves them. It returns nil when there is nothing.
//
// An owner has gone when it isn't in present, the sessions the tool sees right
// now, and has been owner for PresenceTTL. A delivery is taken back while it has
// not landed, its claimer isn't in present and nobody has touched it for
// PresenceTTL: a session that died before its delivery landed does not strand
// it, and a caller whose present was read before another session claimed or
// became owner does not take from it. A delivery that reached the agent is only
// handed out again by SetOwner.
func (s *Store) Claim(ctx context.Context, session string, scopes, present []string) (*Delivery, error) {
	var d *Delivery
	err := s.tx(ctx, func(tx *sql.Tx) error {
		var err error
		d, err = s.ClaimTx(ctx, tx, session, scopes, present)
		return err
	})
	return d, err
}

// ClaimTx is Claim in the tool's own transaction.
func (s *Store) ClaimTx(ctx context.Context, tx *sql.Tx, session string, scopes, present []string) (*Delivery, error) {
	here, err := jsonList(present)
	if err != nil {
		return nil, err
	}
	may, err := jsonList(scopes)
	if err != nil {
		return nil, err
	}
	now := s.Now()
	stale := ms(now.Add(-s.presenceTTL()))
	const own = "COALESCE(o.owner, '') = ?1"
	const gone = `(COALESCE(o.owner, '') = '' OR (o.owner != ?1 AND o.owner NOT IN (SELECT value FROM json_each(?2)) AND o.owned_at <= ?4))`
	for _, mayTake := range []string{own, gone} {
		var id int64
		err := tx.QueryRowContext(ctx, `SELECT d.id FROM thread_deliveries d LEFT JOIN thread_scopes o ON o.scope = d.scope
			WHERE d.state = 'inflight' AND d.shown_at = 0 AND d.touched_at <= ?4
			AND d.session NOT IN (SELECT value FROM json_each(?2))
			AND d.scope IN (SELECT value FROM json_each(?3)) AND `+mayTake+`
			ORDER BY d.id LIMIT 1`, session, here, may, stale).Scan(&id)
		if err == nil {
			_, err = tx.ExecContext(ctx, "UPDATE thread_deliveries SET session = ?, token = ?, touched_at = ?, held = 0 WHERE id = ?", session, reportToken(), ms(now), id)
			if err != nil {
				return nil, err
			}
			return deliveryRead(ctx, tx, s, id)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		var batch int64
		var scope string
		// The ?2 and ?4 no-ops are there because own uses neither, and every
		// argument has to bind.
		err = tx.QueryRowContext(ctx, `SELECT m.batch_id, t.scope FROM thread_messages m
			JOIN thread_threads t ON t.id = m.thread_id
			LEFT JOIN thread_scopes o ON o.scope = t.scope
			LEFT JOIN thread_deliveries d ON d.id = m.delivery_id
			WHERE `+queued+` AND t.scope IN (SELECT value FROM json_each(?3)) AND `+mayTake+`
			AND ?2 = ?2 AND ?4 = ?4
			ORDER BY m.queued_at, m.batch_id LIMIT 1`, session, here, may, stale).Scan(&batch, &scope)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		res, err := tx.ExecContext(ctx, "INSERT INTO thread_deliveries(scope, batch_id, session, state, sent_at, touched_at, token) VALUES (?, ?, ?, 'inflight', ?, ?, ?)",
			scope, batch, session, ms(now), ms(now), reportToken())
		if err != nil {
			return nil, err
		}
		if id, err = res.LastInsertId(); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE thread_messages SET delivery_id = ? WHERE id IN (
			SELECT m.id FROM thread_messages m LEFT JOIN thread_deliveries d ON d.id = m.delivery_id
			WHERE m.batch_id = ? AND `+queued+`)`, id, batch); err != nil {
			return nil, err
		}
		return deliveryRead(ctx, tx, s, id)
	}
	return nil, nil
}

// jsonList is list as JSON for json_each; nil is an empty list, not null.
func jsonList(list []string) (string, error) {
	if list == nil {
		list = []string{}
	}
	b, err := json.Marshal(list)
	return string(b), err
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
	return s.delivery(ctx, s.DB, id)
}

func deliveryRead(ctx context.Context, q querier, s *Store, id int64) (*Delivery, error) {
	d, err := s.delivery(ctx, q, id)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func (s *Store) delivery(ctx context.Context, q querier, id int64) (Delivery, error) {
	var d Delivery
	var sent, touched, fin, shown int64
	err := q.QueryRowContext(ctx, "SELECT id, scope, batch_id, session, state, sent_at, touched_at, finished_at, shown_at, held != 0, token FROM thread_deliveries WHERE id = ?", id).
		Scan(&d.ID, &d.Scope, &d.BatchID, &d.Session, &d.State, &sent, &touched, &fin, &shown, &d.Held, &d.Token)
	if errors.Is(err, sql.ErrNoRows) {
		return d, ErrNotFound
	}
	if err != nil {
		return d, err
	}
	d.SentAt, d.TouchedAt, d.FinishedAt, d.ShownAt = tm(sent), tm(touched), tm(fin), tm(shown)
	d.Stuck = d.State == InFlight && s.Now().Sub(d.TouchedAt) > s.stuckAfter()
	d.Messages, err = messages(ctx, q, "delivery_id = ? ORDER BY batch_pos, id", id)
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
		var err error
		marked, err = s.HeldTx(ctx, tx, session, token)
		return err
	})
	return marked, err
}

// HeldTx is Held in the tool's own transaction.
func (s *Store) HeldTx(ctx context.Context, tx *sql.Tx, session, token string) (bool, error) {
	id, err := tokenDelivery(ctx, tx, session, token)
	if err != nil || id == 0 {
		return false, err
	}
	res, err := tx.ExecContext(ctx, "UPDATE thread_deliveries SET held = 1 WHERE id = ? AND shown_at = 0 AND held = 0", id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// Landed is the harness reporting that the session's delivery with token landed
// in the agent's turn: its sent messages are with the agent, it is no longer
// held, and it is shown. An unknown token, or another session's, does nothing.
// It returns the ids it moved.
func (s *Store) Landed(ctx context.Context, session, token string) ([]int64, error) {
	var moved []int64
	err := s.tx(ctx, func(tx *sql.Tx) error {
		var err error
		moved, err = s.LandedTx(ctx, tx, session, token)
		return err
	})
	return moved, err
}

// LandedTx is Landed in the tool's own transaction.
func (s *Store) LandedTx(ctx context.Context, tx *sql.Tx, session, token string) ([]int64, error) {
	id, err := tokenDelivery(ctx, tx, session, token)
	if err != nil || id == 0 {
		return nil, err
	}
	return landed(ctx, tx, ms(s.Now()), id)
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

// TurnEnded is the harness reporting that the session's turn ended. Each of the
// session's deliveries in flight that has landed ends: its with_agent and
// working messages are no_reply. One that has not landed stays in flight for the
// turn it lands in. It returns the ended deliveries.
func (s *Store) TurnEnded(ctx context.Context, session string) ([]Delivery, error) {
	var out []Delivery
	err := s.tx(ctx, func(tx *sql.Tx) error {
		var err error
		out, err = s.TurnEndedTx(ctx, tx, session)
		return err
	})
	return out, err
}

// TurnEndedTx is TurnEnded in the tool's own transaction.
func (s *Store) TurnEndedTx(ctx context.Context, tx *sql.Tx, session string) ([]Delivery, error) {
	rows, err := tx.QueryContext(ctx, "SELECT id FROM thread_deliveries WHERE session = ? AND state = 'inflight' AND shown_at != 0 ORDER BY id", session)
	if err != nil {
		return nil, err
	}
	var ended []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return nil, err
		}
		ended = append(ended, id)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	_ = rows.Close()
	return s.endTurn(ctx, tx, ended)
}

// endTurn ends the deliveries ids: unsettled messages no_reply, delivery done.
func (s *Store) endTurn(ctx context.Context, tx *sql.Tx, ids []int64) ([]Delivery, error) {
	now := ms(s.Now())
	var out []Delivery
	for _, id := range ids {
		if _, err := move(ctx, tx, now, id, msgstate.NoReply); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE thread_deliveries SET state = ?, finished_at = ? WHERE id = ?", Done, now, id); err != nil {
			return nil, err
		}
		d, err := s.delivery(ctx, tx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

// Refused is a reply the lifecycle refuses, with the reason; the call changed
// nothing. It is also ErrNotFound when the reason is a message the session
// doesn't have.
type Refused struct {
	Reason string
	cause  error
}

func (r Refused) Error() string { return r.Reason }

// Unwrap lets errors.Is find ErrNotFound.
func (r Refused) Unwrap() error { return r.cause }

// Reply is the agent acknowledging messages from its deliveries, all or nothing:
// working (text is the line under the message, and no thread row), answered,
// declined or failed (text is the agent's reply, added to each thread among the
// ids, answering the first id in it, with meta). Every id must be a message
// delivered to session that msgstate.Acknowledge lets move to state; one already
// in the settled state asked for is left as it is, and one already working takes
// the new line. Anything else refuses the whole call (Refused) and changes
// nothing, a message queued for another delivery included. A reply to a message
// still sent brings its whole delivery to the agent (it reached it as one
// event). A delivery ends once every message in it is settled. It returns the
// ids it moved.
func (s *Store) Reply(ctx context.Context, session string, ids []int64, state msgstate.State, text string, meta json.RawMessage) ([]int64, error) {
	var touched []int64
	err := s.tx(ctx, func(tx *sql.Tx) error {
		var err error
		touched, err = s.ReplyTx(ctx, tx, session, ids, state, text, meta)
		return err
	})
	return touched, err
}

// ReplyTx is Reply in the tool's own transaction. A refusal returns before any
// write, so the tool may go on to commit.
func (s *Store) ReplyTx(ctx context.Context, tx *sql.Tx, session string, ids []int64, state msgstate.State, text string, meta json.RawMessage) ([]int64, error) {
	if state != msgstate.Working && !msgstate.Final(state) {
		return nil, Refused{Reason: fmt.Sprintf("state %q: want working, answered, declined or failed", state)}
	}
	if len(ids) == 0 {
		return nil, Refused{Reason: "no message ids"}
	}
	now := ms(s.Now())
	type target struct {
		id, delivery, thread int64
		state                msgstate.State
	}
	var moves []target
	for _, id := range ids {
		var tg target
		var holder, dState string
		err := tx.QueryRowContext(ctx, `SELECT m.id, COALESCE(m.delivery_id, 0), m.thread_id, m.state, COALESCE(d.session, ''), COALESCE(d.state, '')
			FROM thread_messages m LEFT JOIN thread_deliveries d ON d.id = m.delivery_id WHERE m.id = ?`, id).
			Scan(&tg.id, &tg.delivery, &tg.thread, &tg.state, &holder, &dState)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			return nil, Refused{Reason: fmt.Sprintf("no message %d", id), cause: ErrNotFound}
		case err != nil:
			return nil, err
		case holder != session:
			return nil, Refused{Reason: fmt.Sprintf("message %d: not delivered to %s", id, session), cause: ErrNotFound}
		case tg.state == msgstate.Sent && dState != InFlight:
			return nil, Refused{Reason: fmt.Sprintf("message %d: queued, not with %s", id, session), cause: ErrNotFound}
		case tg.state == state && msgstate.Final(state):
			continue
		case tg.state == state:
			// working again: only the line changes
		case !msgstate.Acknowledge(tg.state, state):
			return nil, Refused{Reason: fmt.Sprintf("message %d: is %s and cannot be marked %s", id, tg.state.Label(), state.Label())}
		}
		moves = append(moves, tg)
	}
	var touched []int64
	var threads []int64
	deliveries := map[int64]bool{}
	answers := map[int64]int64{} // thread: the first id moved in it
	for _, tg := range moves {
		if tg.state == msgstate.Sent {
			if _, err := landed(ctx, tx, now, tg.delivery); err != nil {
				return nil, err
			}
		}
		line, settled := text, int64(0)
		if msgstate.Final(state) {
			line, settled = "", now
		}
		if _, err := tx.ExecContext(ctx, "UPDATE thread_messages SET state = ?, line = ?, settled_at = ? WHERE id = ?", state, line, settled, tg.id); err != nil {
			return nil, err
		}
		touched = append(touched, tg.id)
		if _, ok := answers[tg.thread]; !ok {
			answers[tg.thread] = tg.id
			threads = append(threads, tg.thread)
		}
		if tg.state != msgstate.NoReply {
			// A late reply leaves its ended delivery alone.
			deliveries[tg.delivery] = true
		}
	}
	if text != "" && msgstate.Final(state) {
		for _, thread := range threads {
			if _, err := insert(ctx, tx, thread, NewMessage{Author: session, Body: text, Meta: meta, ReplyTo: answers[thread]}, row{now: now}); err != nil {
				return nil, err
			}
		}
	}
	for did := range deliveries {
		if _, err := tx.ExecContext(ctx, "UPDATE thread_deliveries SET touched_at = ?1, shown_at = CASE WHEN shown_at = 0 THEN ?1 ELSE shown_at END WHERE id = ?2", now, did); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE thread_deliveries SET state = ?1, finished_at = ?2 WHERE id = ?3 AND state = 'inflight'
			AND NOT EXISTS(SELECT 1 FROM thread_messages WHERE delivery_id = ?3 AND state IN `+unsettled+`)`, Done, now, did); err != nil {
			return nil, err
		}
	}
	return touched, nil
}
