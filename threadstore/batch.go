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
	now := ms(s.Now())
	var id int64
	err := s.tx(ctx, func(tx *sql.Tx) error {
		var scope string
		if err := tx.QueryRowContext(ctx, "SELECT scope FROM thread_threads WHERE id = ?", thread).Scan(&scope); errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		var batch int64
		err := tx.QueryRowContext(ctx, "SELECT id FROM thread_batches WHERE scope = ? AND state = 'draft'", scope).Scan(&batch)
		if errors.Is(err, sql.ErrNoRows) {
			res, err := tx.ExecContext(ctx, "INSERT INTO thread_batches(scope, state, created_at) VALUES (?, 'draft', ?)", scope, now)
			if err != nil {
				return err
			}
			if batch, err = res.LastInsertId(); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		if id, err = insert(ctx, tx, thread, m, "", Draft, now, 0); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE thread_messages SET batch_id = ?1,
			batch_pos = (SELECT COALESCE(MAX(batch_pos), 0) + 1 FROM thread_messages WHERE batch_id = ?1) WHERE id = ?2`, batch, id)
		return err
	})
	if err != nil {
		return Message{}, err
	}
	return s.Message(ctx, id)
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

// SendBatch queues a draft batch as one unit (one queue time, batch order),
// for owner: the session it goes to first, recorded now, so a later change of
// owner leaves it with the one it was sent for. With owner "", each message
// goes to its thread's owner.
func (s *Store) SendBatch(ctx context.Context, batch int64, owner string) (int, error) {
	now := ms(s.Now())
	var n int64
	err := s.tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE thread_messages SET state = ?1, queued_at = ?2,
			owner = CASE WHEN ?3 != '' THEN ?3 ELSE (SELECT owner FROM thread_threads t WHERE t.id = thread_id) END
			WHERE batch_id = ?4 AND state = 'draft'`, msgstate.Sent, now, owner, batch)
		if err != nil {
			return err
		}
		n, _ = res.RowsAffected()
		if n == 0 {
			return fmt.Errorf("batch %d has no drafts", batch)
		}
		_, err = tx.ExecContext(ctx, "UPDATE thread_batches SET state = 'sent' WHERE id = ?", batch)
		return err
	})
	return int(n), err
}
