package incident

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/rxtted/charon/internal/config"
	"github.com/rxtted/charon/internal/event"
	"github.com/rxtted/charon/internal/lock"
	"github.com/rxtted/charon/internal/store"
)

type fakeWaker struct{ n int }

func (f *fakeWaker) Wake() { f.n++ }

func newCore(t *testing.T) (*Core, *store.Store, *fakeWaker) {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	cfg := config.Config{Channels: map[string]string{"infra": "111"}, Fallback: "111"}
	w := &fakeWaker{}
	return New(s, cfg, lock.New(), w), s, w
}

func fire(ch, key string) event.Event {
	e := event.Event{Source: "grafana", DedupKey: key, Channel: ch, Status: event.Firing, Title: "host down"}
	e.Normalize()
	return e
}

func TestFiringCreatesIncident(t *testing.T) {
	c, s, w := newCore(t)
	if err := c.Handle(context.Background(), fire("infra", "k")); err != nil {
		t.Fatal(err)
	}
	in, _ := s.ActiveByKey("k")
	if in == nil || in.Status != "active" || !in.DesiredPresent || in.Confirmed || in.ChannelID != "111" {
		t.Fatalf("bad incident: %+v", in)
	}
	if in.Heartbeat {
		t.Fatal("first firing should not set heartbeat")
	}
	if w.n != 1 {
		t.Fatalf("waker called %d times", w.n)
	}
}

func TestRepeatFiringNoNewRow(t *testing.T) {
	c, s, _ := newCore(t)
	c.Handle(context.Background(), fire("infra", "k"))
	c.Handle(context.Background(), fire("infra", "k"))
	in, _ := s.ActiveByKey("k")
	if !in.Heartbeat {
		t.Fatal("repeat firing should set heartbeat")
	}
	var count int
	s.DBForTest().QueryRow(`select count(*) from incidents where dedup_key='k'`).Scan(&count)
	if count != 1 {
		t.Fatalf("expected 1 row, got %d", count)
	}
}

func TestResolveSetsStatus(t *testing.T) {
	c, s, _ := newCore(t)
	c.Handle(context.Background(), fire("infra", "k"))
	res := fire("infra", "k")
	res.Status = event.Resolved
	c.Handle(context.Background(), res)
	if in, _ := s.ActiveByKey("k"); in != nil {
		t.Fatal("incident should no longer be active")
	}
}

// a closed row must not block the same key from firing again.
func TestRefireAfterResolve(t *testing.T) {
	c, s, _ := newCore(t)
	if err := c.Handle(context.Background(), fire("infra", "k")); err != nil {
		t.Fatal(err)
	}
	first, _ := s.ActiveByKey("k")
	if first == nil {
		t.Fatal("expected an active incident after the first firing")
	}

	res := fire("infra", "k")
	res.Status = event.Resolved
	if err := c.Handle(context.Background(), res); err != nil {
		t.Fatal(err)
	}
	if in, _ := s.ActiveByKey("k"); in != nil {
		t.Fatal("incident should no longer be active after resolve")
	}

	if err := c.Handle(context.Background(), fire("infra", "k")); err != nil {
		t.Fatalf("second firing after resolve should succeed, got: %v", err)
	}
	second, _ := s.ActiveByKey("k")
	if second == nil {
		t.Fatal("expected a new active incident after the second firing")
	}
	if second.ID == first.ID {
		t.Fatalf("expected a new row, got the same id %d", second.ID)
	}
}

func TestResolveUnknownNoOp(t *testing.T) {
	c, _, w := newCore(t)
	res := fire("infra", "ghost")
	res.Status = event.Resolved
	if err := c.Handle(context.Background(), res); err != nil {
		t.Fatal(err)
	}
	if w.n != 0 {
		t.Fatal("no-op should not wake the converger")
	}
}

// every field the renderer shows must change displayHash, or a repeat firing
// would render new content but never mark the card unconfirmed, so no edit fires.
func TestDisplayHashFields(t *testing.T) {
	base := &store.Incident{Severity: "warning", Title: "t", Body: "b", Host: "h"}
	h0 := displayHash(base)
	mutate := []func(*store.Incident){
		func(i *store.Incident) { i.Severity = "critical" },
		func(i *store.Incident) { i.Title = "x" },
		func(i *store.Incident) { i.Body = "x" },
		func(i *store.Incident) { i.Host = "x" },
		func(i *store.Incident) { i.Labels = map[string]string{"k": "v"} },
		func(i *store.Incident) { now := time.Now(); who := "n"; i.AckedAt, i.AckedBy = &now, &who },
		func(i *store.Incident) { u := time.Now(); i.SnoozedUntil = &u },
	}
	for idx, m := range mutate {
		c := *base
		m(&c)
		if displayHash(&c) == h0 {
			t.Fatalf("mutator %d changed a rendered field but not the hash", idx)
		}
	}
}
