package discord

import (
	"context"
	"log/slog"
	"time"

	"github.com/rotten-division/charon/internal/lock"
	"github.com/rotten-division/charon/internal/store"
)

type Converger struct {
	store *store.Store
	q     *Queue
	coord *lock.Keyed
	wake  chan struct{}
}

func NewConverger(s *store.Store, q *Queue, coord *lock.Keyed) *Converger {
	return &Converger{store: s, q: q, coord: coord, wake: make(chan struct{}, 1)}
}

// Wake is a non-blocking nudge; a pending wake coalesces.
func (c *Converger) Wake() {
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

func (c *Converger) Run(ctx context.Context) {
	tick := time.NewTicker(30 * time.Second) // backstop; the store is the source of truth
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-c.wake:
		case <-tick.C:
		}
		c.pass()
	}
}

func (c *Converger) pass() {
	rows, err := c.store.NeedingConverge()
	if err != nil {
		slog.Error("converge query failed", "err", err)
		return
	}
	for _, in := range rows {
		if err := c.reconcile(in); err != nil {
			slog.Warn("reconcile failed, will retry", "key", in.DedupKey, "err", err)
			return // discord likely degraded; leave the rest for the next pass
		}
	}
}

// reconcile holds the shared key lock for the whole row, so the discord call and
// the write that records it are atomic against the core and the sweeps. it reloads
// the row by id under the lock since dedup_key is no longer unique across rows.
func (c *Converger) reconcile(in *store.Incident) error {
	release := c.coord.Lock(in.DedupKey)
	defer release()
	in, err := c.store.ById(in.ID) // any status: a resolved row still owes a delete
	if err != nil || in == nil {
		return err
	}
	if in.StaleMessageID != "" { // durable repost: clear the old card first
		if err := c.q.DeleteMsg(in.ChannelID, in.StaleMessageID); err != nil {
			return err
		}
		in.StaleMessageID = ""
	}
	switch {
	case in.DesiredPresent && in.MessageID == "":
		id, err := c.q.Post(in.ChannelID, RenderCreate(in))
		if err != nil {
			return err
		}
		now := time.Now()
		in.MessageID, in.Confirmed, in.LastNotifiedAt = id, true, &now
	case in.DesiredPresent && !in.Confirmed:
		if err := c.q.Edit(in.ChannelID, in.MessageID, RenderUpdate(in)); err != nil {
			return err
		}
		in.Confirmed = true
	case !in.DesiredPresent && in.MessageID != "":
		if err := c.q.DeleteMsg(in.ChannelID, in.MessageID); err != nil {
			return err
		}
		in.MessageID, in.Confirmed = "", true
	}
	return c.store.Update(in) // holds the lock, so the version precondition cannot lose here
}
