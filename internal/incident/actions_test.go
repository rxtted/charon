package incident

import (
	"context"
	"testing"
	"time"
)

func TestAcknowledgeStamps(t *testing.T) {
	c, s, _ := newCore(t)
	c.Handle(context.Background(), fire("infra", "k"))
	if err := c.Acknowledge(context.Background(), "k", "noah"); err != nil {
		t.Fatal(err)
	}
	in, _ := s.ActiveByKey("k")
	if in.AckedAt == nil || *in.AckedBy != "noah" || in.Confirmed {
		t.Fatalf("ack not applied: %+v", in)
	}
}

func TestSnoozeSetsUntil(t *testing.T) {
	c, s, _ := newCore(t)
	c.Handle(context.Background(), fire("infra", "k"))
	if err := c.Snooze(context.Background(), "k", "noah", time.Hour); err != nil {
		t.Fatal(err)
	}
	in, _ := s.ActiveByKey("k")
	if in.SnoozedUntil == nil || time.Until(*in.SnoozedUntil) < 55*time.Minute {
		t.Fatalf("snooze not applied: %+v", in)
	}
}

func TestManualResolve(t *testing.T) {
	c, s, _ := newCore(t)
	c.Handle(context.Background(), fire("infra", "k"))
	if err := c.Resolve(context.Background(), "k", "noah"); err != nil {
		t.Fatal(err)
	}
	if in, _ := s.ActiveByKey("k"); in != nil {
		t.Fatal("should be resolved")
	}
}

func TestActionsNoOpOnMissing(t *testing.T) {
	c, _, _ := newCore(t)
	if err := c.Acknowledge(context.Background(), "ghost", "noah"); err != nil {
		t.Fatal(err)
	}
}
