package incident

import (
	"context"
	"testing"
	"time"

	"github.com/rotten-division/charon/internal/config"
	"github.com/rotten-division/charon/internal/lock"
)

func TestSweepStagesRepostForStaleUnacked(t *testing.T) {
	c, s, _ := newCore(t)
	c.Handle(context.Background(), fire("infra", "k"))
	in, _ := s.ActiveByKey("k") // simulate the converger having posted it 5h ago
	old := time.Now().Add(-5 * time.Hour)
	in.MessageID, in.Confirmed, in.LastNotifiedAt = "msg1", true, &old
	s.Update(in)

	r := NewRenotifier(s, config.Config{RenotifyEvery: 4 * time.Hour}, lock.New(), &fakeWaker{})
	if err := r.Sweep(time.Now()); err != nil {
		t.Fatal(err)
	}
	got, _ := s.ActiveByKey("k")
	if got.StaleMessageID != "msg1" || got.MessageID != "" || got.Confirmed {
		t.Fatalf("repost not staged durably: %+v", got)
	}
}

func TestSweepSkipsAcked(t *testing.T) {
	c, s, _ := newCore(t)
	c.Handle(context.Background(), fire("infra", "k"))
	c.Acknowledge(context.Background(), "k", "noah")
	in, _ := s.ActiveByKey("k")
	old := time.Now().Add(-5 * time.Hour)
	in.MessageID, in.Confirmed, in.LastNotifiedAt = "msg1", true, &old
	s.Update(in)
	r := NewRenotifier(s, config.Config{RenotifyEvery: 4 * time.Hour}, lock.New(), &fakeWaker{})
	r.Sweep(time.Now())
	if got, _ := s.ActiveByKey("k"); got.StaleMessageID != "" {
		t.Fatal("acked incident must not be reposted")
	}
}
