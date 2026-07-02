package discord

import (
	"context"
	"testing"
	"time"

	dgo "github.com/disgoorg/disgo/discord"
)

type fakeSender struct {
	posts       int
	onPost      func(channelID string)
	onDelete    func()
	onEdit      func() error
	onDeleteErr func() error
}

func (f *fakeSender) Post(_ context.Context, channelID string, _ dgo.MessageCreate) (string, error) {
	f.posts++
	if f.onPost != nil {
		f.onPost(channelID)
	}
	return "msg", nil
}
func (f *fakeSender) Edit(_ context.Context, _, _ string, _ dgo.MessageUpdate) error {
	if f.onEdit != nil {
		return f.onEdit()
	}
	return nil
}
func (f *fakeSender) DeleteMsg(_ context.Context, _, _ string) error {
	if f.onDelete != nil {
		f.onDelete()
	}
	if f.onDeleteErr != nil {
		return f.onDeleteErr()
	}
	return nil
}

func TestQueuePacesPerChannel(t *testing.T) {
	f := &fakeSender{}
	q := NewQueue(f, 50*time.Millisecond, 1)
	start := time.Now()
	for i := 0; i < 3; i++ {
		if _, err := q.Post(context.Background(), "chan-a", dgo.MessageCreate{}); err != nil {
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
