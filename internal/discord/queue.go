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
	Post(channelID string, mc dgo.MessageCreate) (string, error)
	Edit(channelID, msgID string, mu dgo.MessageUpdate) error
	DeleteMsg(channelID, msgID string) error
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

func (q *Queue) wait(channelID string) { _ = q.limiter(channelID).Wait(context.Background()) }

func (q *Queue) Post(channelID string, mc dgo.MessageCreate) (string, error) {
	q.wait(channelID)
	id, err := q.s.Post(channelID, mc)
	q.degraded.Store(err != nil)
	return id, err
}

func (q *Queue) Edit(channelID, msgID string, mu dgo.MessageUpdate) error {
	q.wait(channelID)
	err := q.s.Edit(channelID, msgID, mu)
	q.degraded.Store(err != nil)
	return err
}

func (q *Queue) DeleteMsg(channelID, msgID string) error {
	q.wait(channelID)
	err := q.s.DeleteMsg(channelID, msgID)
	q.degraded.Store(err != nil)
	return err
}

func (q *Queue) Degraded() bool { return q.degraded.Load() }
