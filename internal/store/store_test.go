package store

import (
	"path/filepath"
	"testing"
)

func TestOpenCreatesSchema(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var n int
	err = db.db.QueryRow(
		`select count(*) from sqlite_master where type='table' and name='incidents'`,
	).Scan(&n)
	if err != nil || n != 1 {
		t.Fatalf("incidents table missing: n=%d err=%v", n, err)
	}
	var mode string
	if err := db.db.QueryRow(`pragma journal_mode`).Scan(&mode); err != nil {
		t.Fatal(err)
	}
	if mode != "wal" {
		t.Fatalf("journal_mode = %q, want wal", mode)
	}
}
