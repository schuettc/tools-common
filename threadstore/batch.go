package threadstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/subaud-tools/tools-common/msgstate"
)

// Draft adds a message to the draft batch of its thread's scope, started if
// the scope has none. It stays a draft, in no queue, until SendBatch: drafts go
// out together.
func (s *Store) Draft(ctx context.Context, thread int64, m NewMessage) (Message, error) {
	var out Message
	err := s.tx(ctx, func(tx *sql.Tx) error {
		var err error
		out, err = s.draft(ctx, tx, thread, m)
		return err
	})
	return out, err
}

func (s *Store) draft(ctx context.Context, tx *sql.Tx, thread int64, m NewMessage) (Message, error) {
	now := ms(s.Now())
	scope, err := scopeOf(ctx, tx, thread)
	if err != nil {
		return Message{}, err
	}
	var batch int64
	err = tx.QueryRowContext(ctx, "SELECT id FROM thread_batches WHERE scope = ? AND state = 'draft'", scope).Scan(&batch)
	if errors.Is(err, sql.ErrNoRows) {
		res, err := tx.ExecContext(ctx, "INSERT INTO thread_batches(scope, state, created_at) VALUES (?, 'draft', ?)", scope, now)
		if err != nil {
			return Message{}, err
		}
		if batch, err = res.LastInsertId(); err != nil {
			return Message{}, err
		}
	} else if err != nil {
		return Message{}, err
	}
	var pos int
	if err := tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(batch_pos), 0) + 1 FROM thread_messages WHERE batch_id = ?", batch).Scan(&pos); err != nil {
		return Message{}, err
	}
	id, err := insert(ctx, tx, thread, m, row{state: Draft, batch: batch, pos: pos, now: now})
	if err != nil {
		return Message{}, err
	}
	return message(ctx, tx, id)
}

// DraftBatch returns a scope's draft batch and its messages in order (id 0 when
// there is none).
func (s *Store) DraftBatch(ctx context.Context, scope string) (int64, []Message, error) {
	var batch int64
	err := s.DB.QueryRowContext(ctx, "SELECT id FROM thread_batches WHERE scope = ? AND state = 'draft'", scope).Scan(&batch)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil, nil
	}
	if err != nil {
		return 0, nil, err
	}
	out, err := messages(ctx, s.DB, "batch_id = ? AND state = 'draft' ORDER BY batch_pos", batch)
	return batch, out, err
}

// EditDraft changes a draft's text.
func (s *Store) EditDraft(ctx context.Context, id int64, body string) error {
	return s.draftExec(ctx, "UPDATE thread_messages SET body = ? WHERE id = ? AND state = 'draft'", body, id)
}

// RemoveDraft deletes a draft.
func (s *Store) RemoveDraft(ctx context.Context, id int64) error {
	return s.draftExec(ctx, "DELETE FROM thread_messages WHERE id = ? AND state = 'draft'", id)
}

func (s *Store) draftExec(ctx context.Context, query string, args ...any) error {
	res, err := s.DB.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ReorderBatch sets a draft batch's order; ids must be exactly its drafts.
func (s *Store) ReorderBatch(ctx context.Context, batch int64, ids []int64) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		var n int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM thread_messages WHERE batch_id = ? AND state = 'draft'", batch).Scan(&n); err != nil {
			return err
		}
		if n != len(ids) {
			return fmt.Errorf("reorder needs all %d drafts, got %d", n, len(ids))
		}
		for i, id := range ids {
			res, err := tx.ExecContext(ctx, "UPDATE thread_messages SET batch_pos = ? WHERE id = ? AND batch_id = ? AND state = 'draft'", i+1, id, batch)
			if err != nil {
				return err
			}
			if k, _ := res.RowsAffected(); k == 0 {
				return fmt.Errorf("message %d is not a draft in batch %d", id, batch)
			}
		}
		return nil
	})
}

// SendBatch queues a draft batch as one unit (one queue time, batch order), for
// its scope's owner at the time it is claimed. It returns how many went.
func (s *Store) SendBatch(ctx context.Context, batch int64) (int, error) {
	var n int
	err := s.tx(ctx, func(tx *sql.Tx) error {
		var err error
		n, err = s.sendBatch(ctx, tx, batch)
		return err
	})
	return n, err
}

func (s *Store) sendBatch(ctx context.Context, tx *sql.Tx, batch int64) (int, error) {
	now := ms(s.Now())
	res, err := tx.ExecContext(ctx, "UPDATE thread_messages SET state = ?, queued_at = ? WHERE batch_id = ? AND state = 'draft'", msgstate.Sent, now, batch)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	if n == 0 {
		return 0, fmt.Errorf("batch %d has no drafts", batch)
	}
	_, err = tx.ExecContext(ctx, "UPDATE thread_batches SET state = 'sent', sent_at = ? WHERE id = ?", now, batch)
	return int(n), err
}
