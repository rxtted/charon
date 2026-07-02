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

func TestInsertActiveByKey(t *testing.T) {
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

// the boot orphan sweep's keep-list must include
// only live cards of active incidents; resolved rows and unposted ones don't belong.
func TestActiveMessageIDs(t *testing.T) {
	s := newStore(t)
	active := &Incident{DedupKey: "k1", Channel: "infra", Severity: "warning", Status: "active",
		Version: 1, Title: "t", MessageID: "m1", CreatedAt: time.Now()}
	unposted := &Incident{DedupKey: "k2", Channel: "infra", Severity: "warning", Status: "active",
		Version: 1, Title: "t", MessageID: "", CreatedAt: time.Now()}
	resolved := &Incident{DedupKey: "k3", Channel: "infra", Severity: "warning", Status: "resolved",
		Version: 1, Title: "t", MessageID: "m3", CreatedAt: time.Now()}
	for _, in := range []*Incident{active, unposted, resolved} {
		if err := s.Insert(in); err != nil {
			t.Fatal(err)
		}
	}
	ids, err := s.ActiveMessageIDs()
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || !ids["m1"] {
		t.Fatalf("ActiveMessageIDs() = %v, want only m1", ids)
	}
}

func TestUpdateRejectsStale(t *testing.T) {
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
