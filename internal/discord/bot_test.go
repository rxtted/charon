package discord

import (
	"context"
	"testing"
	"time"

	dgo "github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/snowflake/v2"
)

type fakeActions struct {
	acked, resolved, snoozed string
	dur                      time.Duration
}

func (f *fakeActions) Acknowledge(_ context.Context, key, _ string) error { f.acked = key; return nil }
func (f *fakeActions) Resolve(_ context.Context, key, _ string) error     { f.resolved = key; return nil }
func (f *fakeActions) Snooze(_ context.Context, key, _ string, d time.Duration) error {
	f.snoozed, f.dur = key, d
	return nil
}

func TestDispatchRoutes(t *testing.T) {
	a := &fakeActions{}
	_ = dispatch(a, "ack", "k1", "noah", 0)
	_ = dispatch(a, "resolve", "k2", "noah", 0)
	_ = dispatch(a, "snooze", "k3", "noah", time.Hour)
	if a.acked != "k1" || a.resolved != "k2" || a.snoozed != "k3" || a.dur != time.Hour {
		t.Fatalf("bad routing: %+v", a)
	}
	if err := dispatch(a, "bogus", "k", "noah", 0); err == nil {
		t.Fatal("unknown action should error")
	}
}

func TestDispatchNilIsSafe(t *testing.T) {
	if err := dispatch(nil, "ack", "k", "noah", 0); err != nil {
		t.Fatal("nil actions should be a no-op, not a panic")
	}
}

// TestOrphanIDs covers I1's selection logic: only self-authored messages not in
// keep are orphans; messages from other users and kept messages are left alone.
func TestOrphanIDs(t *testing.T) {
	self := snowflake.ID(100)
	other := snowflake.ID(200)
	msgs := []dgo.Message{
		{ID: snowflake.ID(1), Author: dgo.User{ID: self}},  // orphan: self-authored, not kept
		{ID: snowflake.ID(2), Author: dgo.User{ID: self}},  // kept: still backs an active incident
		{ID: snowflake.ID(3), Author: dgo.User{ID: other}}, // not ours: never touch it
	}
	keep := map[string]bool{"2": true}

	got := orphanIDs(msgs, self, keep)
	if len(got) != 1 || got[0] != snowflake.ID(1) {
		t.Fatalf("orphanIDs() = %v, want [1]", got)
	}
}

func TestSnoozeValueParse(t *testing.T) {
	got, err := parseSnoozeValue("1h")
	if err != nil || got.Hours() != 1 {
		t.Fatalf("parse 1h: %v %v", got, err)
	}
	if _, err := parseSnoozeValue("garbage"); err == nil {
		t.Fatal("expected error on bad duration")
	}
}
