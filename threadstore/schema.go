package threadstore

import "github.com/subaud-tools/tools-common/sqlitedb"

// schema is the store's first step. Every table is prefixed thread_, so it
// can't collide with a tool's own sends, threads or messages while the tool
// moves its data.
const schema = `
CREATE TABLE thread_scopes (
  scope    TEXT PRIMARY KEY,
  owner    TEXT NOT NULL DEFAULT '',
  owned_at INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX thread_scopes_owner ON thread_scopes(owner);
CREATE TABLE thread_threads (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  scope      TEXT NOT NULL,
  name       TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  UNIQUE (scope, name)
);
CREATE TABLE thread_batches (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  scope      TEXT NOT NULL,
  state      TEXT NOT NULL DEFAULT 'draft',
  created_at INTEGER NOT NULL,
  sent_at    INTEGER NOT NULL DEFAULT 0
);
CREATE UNIQUE INDEX thread_batches_one_draft ON thread_batches(scope) WHERE state = 'draft';
CREATE TABLE thread_deliveries (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  scope       TEXT NOT NULL,
  batch_id    INTEGER NOT NULL REFERENCES thread_batches(id),
  session     TEXT NOT NULL,
  state       TEXT NOT NULL,
  token       TEXT NOT NULL,
  held        INTEGER NOT NULL DEFAULT 0,
  sent_at     INTEGER NOT NULL,
  touched_at  INTEGER NOT NULL,
  finished_at INTEGER NOT NULL DEFAULT 0,
  shown_at    INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX thread_deliveries_session ON thread_deliveries(session, state);
CREATE INDEX thread_deliveries_scope ON thread_deliveries(scope, state);
CREATE INDEX thread_deliveries_batch ON thread_deliveries(batch_id);
CREATE TABLE thread_messages (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  thread_id   INTEGER NOT NULL REFERENCES thread_threads(id),
  author      TEXT NOT NULL,
  body        TEXT NOT NULL,
  meta        TEXT NOT NULL DEFAULT '',
  reply_to    INTEGER REFERENCES thread_messages(id),
  batch_id    INTEGER REFERENCES thread_batches(id),
  batch_pos   INTEGER NOT NULL DEFAULT 0,
  delivery_id INTEGER REFERENCES thread_deliveries(id),
  state       TEXT NOT NULL DEFAULT '',
  line        TEXT NOT NULL DEFAULT '',
  created_at  INTEGER NOT NULL,
  queued_at   INTEGER NOT NULL DEFAULT 0,
  settled_at  INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX thread_messages_thread ON thread_messages(thread_id, id);
CREATE INDEX thread_messages_state ON thread_messages(state);
CREATE INDEX thread_messages_delivery ON thread_messages(delivery_id);
CREATE INDEX thread_messages_batch ON thread_messages(batch_id, batch_pos);
CREATE INDEX thread_messages_reply_to ON thread_messages(reply_to);
`

// Migrations is the store's schema, as steps a tool puts in its own database's
// list. The list only grows, and sqlitedb numbers a database's steps by their
// place in the tool's list, so a tool takes steps by position: Migrations[0],
// and later Migrations[1] after the steps of its own it has added by then, never
// the whole list. Appending the whole list would let a new store step shift the
// tool's own later steps, and a database already past them would skip it.
var Migrations = []sqlitedb.Step{
	sqlitedb.SQL(schema),
}
