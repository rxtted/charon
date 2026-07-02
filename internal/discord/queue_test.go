package discord

import (
	"testing"
	"time"

	dgo "github.com/disgoorg/disgo/discord"
)

type fakeSender struct {
	posts    int
	onDelete func()
}

func (f *fakeSender) Post(_ string, _ dgo.MessageCreate) (string, error) {
	f.posts++
	return "msg", nil
}
func (f *fakeSender) Edit(_, _ string, _ dgo.MessageUpdate) error { return nil }
func (f *fakeSender) DeleteMsg(_, _ string) error {
	if f.onDelete != nil {
		f.onDelete()
	}
	return nil
}

func TestQueuePacesPerChannel(t *testing.T) {
	f := &fakeSender{}
	q := NewQueue(f, 50*time.Millisecond, 1)
	start := time.Now()
	for i := 0; i < 3; i++ {
		if _, err := q.Post("chan-a", dgo.MessageCreate{}); err != nil {
			t.Fatal(err)
		}
	}
	if elapsed := time.Since(start); elapsed < 90*time.Millisecond {
		t.Fatalf("3 posts should pace to ~100ms, took %s", elapsed)
	}
	if f.posts != 3 {
		t.Fatalf("posts = %d", f.posts)
	}
}
