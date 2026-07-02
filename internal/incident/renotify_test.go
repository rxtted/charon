package incident

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/rotten-division/charon/internal/config"
	"github.com/rotten-division/charon/internal/lock"
	"github.com/rotten-division/charon/internal/store"
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

func TestSweepSkipsFreshlySnoozed(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	cfg := config.Config{Channels: map[string]string{"infra": "111"}, Fallback: "111"}
	coord := lock.New()
	c := New(s, cfg, coord, &fakeWaker{})

	c.Handle(context.Background(), fire("infra", "k"))
	in, _ := s.ActiveByKey("k")
	old := time.Now().Add(-5 * time.Hour)
	in.MessageID, in.Confirmed, in.LastNotifiedAt = "msg1", true, &old
	s.Update(in)

	c.Snooze(context.Background(), "k", "noah", 1*time.Hour)

	r := NewRenotifier(s, config.Config{RenotifyEvery: 4 * time.Hour}, coord, &fakeWaker{})
	if err := r.Sweep(time.Now()); err != nil {
		t.Fatal(err)
	}

	got, _ := s.ActiveByKey("k")
	if got.StaleMessageID != "" {
		t.Fatalf("snoozed incident must not be reposted; got StaleMessageID=%q", got.StaleMessageID)
	}
	if got.SnoozedUntil == nil {
		t.Fatal("SnoozedUntil must still be set")
	}
}

func TestRepostBlocked(t *testing.T) {
	now := time.Now()
	ackedTime := now.Add(-1 * time.Minute)
	snoozeUntil := now.Add(1 * time.Hour)

	tests := []struct {
		name     string
		incident *store.Incident
		want     bool
	}{
		{
			name: "acked incident is blocked",
			incident: &store.Incident{
				AckedAt:   &ackedTime,
				MessageID: "msg1",
			},
			want: true,
		},
		{
			name: "incident with no message is blocked",
			incident: &store.Incident{
				MessageID: "",
			},
			want: true,
		},
		{
			name: "snoozed into the future is blocked",
			incident: &store.Incident{
				MessageID:    "msg1",
				SnoozedUntil: &snoozeUntil,
				AckedAt:      nil,
			},
			want: true,
		},
		{
			name: "plain due incident is not blocked",
			incident: &store.Incident{
				MessageID:    "msg1",
				AckedAt:      nil,
				SnoozedUntil: nil,
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := repostBlocked(tt.incident, now)
			if got != tt.want {
				t.Errorf("repostBlocked() = %v, want %v", got, tt.want)
			}
		})
	}
}
