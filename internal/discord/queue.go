package discord

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	dgo "github.com/disgoorg/disgo/discord"
	"golang.org/x/time/rate"
)

type sender interface {
	Post(ctx context.Context, channelID string, mc dgo.MessageCreate) (string, error)
	Edit(ctx context.Context, channelID, msgID string, mu dgo.MessageUpdate) error
	DeleteMsg(ctx context.Context, channelID, msgID string) error
}

type Queue struct {
	s        sender
	every    time.Duration
	burst    int
	limiters sync.Map // channelID string -> *rate.Limiter
	degraded atomic.Bool
}

func NewQueue(s sender, perChannelEvery time.Duration, burst int) *Queue {
	return &Queue{s: s, every: perChannelEvery, burst: burst}
}

func (q *Queue) limiter(channelID string) *rate.Limiter {
	if l, ok := q.limiters.Load(channelID); ok {
		return l.(*rate.Limiter)
	}
	fresh := rate.NewLimiter(rate.Every(q.every), q.burst)
	actual, _ := q.limiters.LoadOrStore(channelID, fresh)
	return actual.(*rate.Limiter)
}

func (q *Queue) wait(ctx context.Context, channelID string) error {
	return q.limiter(channelID).Wait(ctx)
}

func (q *Queue) Post(ctx context.Context, channelID string, mc dgo.MessageCreate) (string, error) {
	if err := q.wait(ctx, channelID); err != nil {
		return "", err
	}
	id, err := q.s.Post(ctx, channelID, mc)
	q.degraded.Store(err != nil)
	return id, err
}

func (q *Queue) Edit(ctx context.Context, channelID, msgID string, mu dgo.MessageUpdate) error {
	if err := q.wait(ctx, channelID); err != nil {
		return err
	}
	err := q.s.Edit(ctx, channelID, msgID, mu)
	q.degraded.Store(err != nil)
	return err
}

func (q *Queue) DeleteMsg(ctx context.Context, channelID, msgID string) error {
	if err := q.wait(ctx, channelID); err != nil {
		return err
	}
	err := q.s.DeleteMsg(ctx, channelID, msgID)
	q.degraded.Store(err != nil)
	return err
}

func (q *Queue) Degraded() bool { return q.degraded.Load() }
