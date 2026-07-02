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
    id               integer primary key autoincrement,
    dedup_key        text not null,
    channel          text not null,
    channel_id       text not null default '',
    source           text not null default '',
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
create unique index if not exists idx_active_key on incidents(dedup_key) where status='active';
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
	// databases created before the source column need it added; the create-table
	// above is a no-op on an existing table.
	has, err := hasColumn(db, "source")
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("check source column: %w", err)
	}
	if !has {
		if _, err := db.Exec(`alter table incidents add column source text not null default ''`); err != nil {
			db.Close()
			return nil, fmt.Errorf("add source column: %w", err)
		}
	}
	return &Store{db: db}, nil
}

func hasColumn(db *sql.DB, col string) (bool, error) {
	rows, err := db.Query(`pragma table_info(incidents)`)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notnull, pk int
		var name, ctype string
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return false, err
		}
		if name == col {
			return true, nil
		}
	}
	return false, rows.Err()
}

func (s *Store) Close() error { return s.db.Close() }

// DBForTest exposes the underlying *sql.DB for assertions in this module's tests.
func (s *Store) DBForTest() *sql.DB { return s.db }
