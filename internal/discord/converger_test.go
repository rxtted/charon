package discord

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/disgoorg/disgo/rest"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rxtted/cheron/internal/lock"
	"github.com/rxtted/cheron/internal/store"
)

func newConv(t *testing.T) (*Converger, *store.Store, *fakeSender) {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	f := &fakeSender{}
	errs := prometheus.NewCounter(prometheus.CounterOpts{Name: "test_converge_errors"})
	return NewConverger(s, NewQueue(f, time.Millisecond, 100), lock.New(), errs), s, f
}

func TestReconcilePostsThenMarksConfirmed(t *testing.T) {
	cv, s, f := newConv(t)
	in := &store.Incident{DedupKey: "k", Channel: "infra", ChannelID: "111", Severity: "warning",
		Status: "active", Version: 1, Title: "t", DesiredPresent: true, Confirmed: false, CreatedAt: time.Now()}
	s.Insert(in)
	if err := cv.reconcile(context.Background(), in); err != nil {
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
	if err := cv.reconcile(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	if del != 1 {
		t.Fatalf("expected 1 delete, got %d", del)
	}
}

// TestReconcileDeleteNotFoundConverges: a delete-not-found means the
// message is already gone, so it must count as a success.
func TestReconcileDeleteNotFoundConverges(t *testing.T) {
	cv, s, f := newConv(t)
	f.onDeleteErr = func() error { return &rest.Error{Code: rest.JSONErrorCodeUnknownMessage} }
	in := &store.Incident{DedupKey: "k", ChannelID: "111", Status: "resolved", Version: 1,
		DesiredPresent: false, Confirmed: false, MessageID: "msg", CreatedAt: time.Now()}
	s.Insert(in)
	if err := cv.reconcile(context.Background(), in); err != nil {
		t.Fatalf("not-found delete should not propagate an error, got %v", err)
	}
	got, _ := s.ByID(in.ID)
	if got.MessageID != "" || !got.Confirmed {
		t.Fatalf("row should be converged with message_id cleared: %+v", got)
	}
}

// TestReconcileEditNotFoundReposts: an edit-not-found means the card
// vanished, so message_id clears and confirmed goes false so it reposts.
func TestReconcileEditNotFoundReposts(t *testing.T) {
	cv, s, f := newConv(t)
	f.onEdit = func() error { return &rest.Error{Code: rest.JSONErrorCodeUnknownMessage} }
	in := &store.Incident{DedupKey: "k", ChannelID: "111", Status: "active", Version: 1,
		DesiredPresent: true, Confirmed: false, MessageID: "stale-msg", CreatedAt: time.Now()}
	s.Insert(in)
	if err := cv.reconcile(context.Background(), in); err != nil {
		t.Fatalf("not-found edit should not propagate an error, got %v", err)
	}
	got, _ := s.ByID(in.ID)
	if got.MessageID != "" || got.Confirmed {
		t.Fatalf("row should be cleared to repost: %+v", got)
	}
}

func TestIsNotFound(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"unknown message code", &rest.Error{Code: rest.JSONErrorCodeUnknownMessage}, true},
		{"other json error code", &rest.Error{Code: rest.JSONErrorCodeMissingAccess}, false},
		{"plain error", errors.New("boom"), false},
		{"nil", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isNotFound(tc.err); got != tc.want {
				t.Fatalf("isNotFound(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// TestPassContinuesAfterRowError: one bad row must not starve the
// rest of the sweep behind it. the bad row's error is a genuine store-level
// conflict (a concurrent writer bumps its version mid-post), so the queue never
// looks degraded and the pass must not bail early.
func TestPassContinuesAfterRowError(t *testing.T) {
	cv, s, f := newConv(t)
	bad := &store.Incident{DedupKey: "bad", ChannelID: "111", Status: "active", Version: 1,
		DesiredPresent: true, Confirmed: false, CreatedAt: time.Now()}
	good := &store.Incident{DedupKey: "good", ChannelID: "222", Status: "active", Version: 1,
		DesiredPresent: true, Confirmed: false, CreatedAt: time.Now()}
	s.Insert(bad)
	s.Insert(good)

	f.onPost = func(channelID string) {
		if channelID == bad.ChannelID {
			if _, err := s.DBForTest().Exec(`update incidents set version = version + 1 where id = ?`, bad.ID); err != nil {
				t.Fatal(err)
			}
		}
	}

	cv.pass(context.Background())

	gotBad, _ := s.ByID(bad.ID)
	gotGood, _ := s.ActiveByKey("good")
	if gotBad.Confirmed {
		t.Fatalf("bad row should still be unconverged: %+v", gotBad)
	}
	if !gotGood.Confirmed || gotGood.MessageID == "" {
		t.Fatalf("good row should have converged despite the bad row's error: %+v", gotGood)
	}
	if cv.q.Degraded() {
		t.Fatal("a store-level conflict should not mark the discord queue degraded")
	}
}

// TestPassStopsOnCancelledContext covers fix 2: a shutdown must abort the
// remaining rows of a pass promptly rather than running each one out to its
// own reconcile timeout.
func TestPassStopsOnCancelledContext(t *testing.T) {
	cv, s, f := newConv(t)
	in := &store.Incident{DedupKey: "k", ChannelID: "111", Status: "active", Version: 1,
		DesiredPresent: true, Confirmed: false, CreatedAt: time.Now()}
	s.Insert(in)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cv.pass(ctx)

	if f.posts != 0 {
		t.Fatalf("a cancelled context must not post, got %d posts", f.posts)
	}
	got, _ := s.ActiveByKey("k")
	if got.Confirmed {
		t.Fatalf("row should still be unconverged: %+v", got)
	}
}
