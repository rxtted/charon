package store

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestInsertAndActiveByKey(t *testing.T) {
	s := newStore(t)
	in := &Incident{DedupKey: "k1", Channel: "infra", Severity: "warning", Status: "active", Version: 1, Title: "t", CreatedAt: time.Now()}
	if err := s.Insert(in); err != nil {
		t.Fatal(err)
	}
	got, err := s.ActiveByKey("k1")
	if err != nil || got == nil {
		t.Fatalf("active lookup: %v %v", got, err)
	}
	if got.Title != "t" {
		t.Fatalf("title = %q", got.Title)
	}
	missing, err := s.ActiveByKey("nope")
	if err != nil || missing != nil {
		t.Fatalf("expected nil,nil got %v,%v", missing, err)
	}
}

func TestUpdateRejectsStaleVersion(t *testing.T) {
	s := newStore(t)
	in := &Incident{DedupKey: "k1", Channel: "infra", Severity: "warning", Status: "active", Version: 1, Title: "t", CreatedAt: time.Now()}
	if err := s.Insert(in); err != nil {
		t.Fatal(err)
	}
	stale := *in // still version 1
	fresh := *in
	if err := s.Update(&fresh); err != nil { // bumps to 2
		t.Fatal(err)
	}
	if err := s.Update(&stale); !errors.Is(err, ErrStale) {
		t.Fatalf("expected ErrStale, got %v", err)
	}
}
