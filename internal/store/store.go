package store

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

const schema = `
create table if not exists incidents (
    dedup_key        text primary key,
    channel          text not null,
    channel_id       text not null default '',
    severity         text not null,
    status           text not null,          -- active | resolved
    version          integer not null default 1,
    title            text not null,
    body             text not null default '',
    host             text not null default '',
    link             text not null default '',
    labels           text not null default '{}',
    desired_present  integer not null default 1,
    content_hash     text not null default '',
    message_id       text not null default '',
    stale_message_id text not null default '',   -- an old card a repost still owes a delete
    created_at       integer not null,
    last_seen_firing integer not null default 0,
    confirmed        integer not null default 1,
    heartbeat        integer not null default 0,   -- set once a repeat firing proves a heartbeat
    acked_at         integer,
    acked_by         text,
    snoozed_until    integer,
    last_notified_at integer,
    resolved_at      integer
);
create index if not exists idx_active on incidents(status) where status='active';
create index if not exists idx_sweeps on incidents(status, last_notified_at, snoozed_until, last_seen_firing);
`

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(on)")
	if err != nil {
		return nil, fmt.Errorf("open sqlite %s: %w", path, err)
	}
	db.SetMaxOpenConns(1) // single writer; the core serializes per dedup key on top
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

// DBForTest exposes the underlying *sql.DB for assertions in this module's tests.
func (s *Store) DBForTest() *sql.DB { return s.db }
