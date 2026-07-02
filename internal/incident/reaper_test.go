package incident

import (
	"context"
	"testing"
	"time"

	"github.com/rxtted/charon/internal/config"
	"github.com/rxtted/charon/internal/lock"
)

func TestReaperClosesStale(t *testing.T) {
	c, s, _ := newCore(t)
	c.Handle(context.Background(), fire("infra", "k"))
	c.Handle(context.Background(), fire("infra", "k")) // establishes heartbeat
	in, _ := s.ActiveByKey("k")
	in.LastSeenFiring = time.Now().Add(-1 * time.Hour)
	s.Update(in)

	r := NewReaper(s, config.Config{ReaperGrace: 20 * time.Minute}, lock.New(), &fakeWaker{})
	if err := r.Sweep(time.Now()); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.ActiveByKey("k"); got != nil {
		t.Fatal("stale heartbeat incident should be reaped")
	}
}

func TestReaperSkipsOneShots(t *testing.T) {
	c, s, _ := newCore(t)
	c.Handle(context.Background(), fire("apps", "oneshot1")) // one firing: no heartbeat
	in, _ := s.ActiveByKey("oneshot1")
	in.LastSeenFiring = time.Now().Add(-1 * time.Hour)
	s.Update(in)
	r := NewReaper(s, config.Config{ReaperGrace: 20 * time.Minute}, lock.New(), &fakeWaker{})
	r.Sweep(time.Now())
	if got, _ := s.ActiveByKey("oneshot1"); got == nil {
		t.Fatal("one-shot must never be reaped")
	}
}
