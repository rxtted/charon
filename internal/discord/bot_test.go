package discord

import (
	"context"
	"testing"
	"time"
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

func TestSnoozeValueParse(t *testing.T) {
	got, err := parseSnoozeValue("1h")
	if err != nil || got.Hours() != 1 {
		t.Fatalf("parse 1h: %v %v", got, err)
	}
	if _, err := parseSnoozeValue("garbage"); err == nil {
		t.Fatal("expected error on bad duration")
	}
}
