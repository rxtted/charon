package discord

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/rotten-division/charon/internal/lock"
	"github.com/rotten-division/charon/internal/store"
)

func newConv(t *testing.T) (*Converger, *store.Store, *fakeSender) {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	f := &fakeSender{}
	return NewConverger(s, NewQueue(f, time.Millisecond, 100), lock.New()), s, f
}

func TestReconcilePostsThenMarksConfirmed(t *testing.T) {
	cv, s, f := newConv(t)
	in := &store.Incident{DedupKey: "k", Channel: "infra", ChannelID: "111", Severity: "warning",
		Status: "active", Version: 1, Title: "t", DesiredPresent: true, Confirmed: false, CreatedAt: time.Now()}
	s.Insert(in)
	if err := cv.reconcile(in.DedupKey); err != nil {
		t.Fatal(err)
	}
	if f.posts != 1 {
		t.Fatalf("expected 1 post, got %d", f.posts)
	}
	got, _ := s.ActiveByKey("k")
	if !got.Confirmed || got.MessageID != "msg" || got.LastNotifiedAt == nil {
		t.Fatalf("row not converged: %+v", got)
	}
}

func TestReconcileDeletesWhenAbsent(t *testing.T) {
	cv, s, f := newConv(t)
	del := 0
	f.onDelete = func() { del++ }
	in := &store.Incident{DedupKey: "k", ChannelID: "111", Status: "resolved", Version: 1,
		DesiredPresent: false, Confirmed: false, MessageID: "msg", CreatedAt: time.Now()}
	s.Insert(in)
	if err := cv.reconcile(in.DedupKey); err != nil {
		t.Fatal(err)
	}
	if del != 1 {
		t.Fatalf("expected 1 delete, got %d", del)
	}
}
