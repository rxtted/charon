package incident

import (
	"context"
	"time"

	"github.com/rxtted/charon/internal/card"
	"github.com/rxtted/charon/internal/config"
	"github.com/rxtted/charon/internal/event"
	"github.com/rxtted/charon/internal/lock"
	"github.com/rxtted/charon/internal/store"
)

type Waker interface{ Wake() }

type Core struct {
	store *store.Store
	cfg   config.Config
	wake  Waker
	coord *lock.Keyed
}

func New(s *store.Store, cfg config.Config, coord *lock.Keyed, w Waker) *Core {
	return &Core{store: s, cfg: cfg, wake: w, coord: coord}
}

func (c *Core) Handle(ctx context.Context, ev event.Event) error {
	release := c.coord.Lock(ev.DedupKey)
	defer release()

	cur, err := c.store.ActiveByKey(ev.DedupKey)
	if err != nil {
		return err
	}
	switch ev.Status {
	case event.Resolved:
		if cur == nil {
			return nil // unknown or already-closed: no-op
		}
		markResolved(cur)
		if err := c.store.Update(cur); err != nil {
			return err
		}
	default: // firing
		if cur == nil {
			if err := c.store.Insert(c.newIncident(ev)); err != nil {
				return err
			}
		} else {
			c.applyFiring(cur, ev)
			if err := c.store.Update(cur); err != nil {
				return err
			}
		}
	}
	c.wake.Wake()
	return nil
}

func (c *Core) newIncident(ev event.Event) *store.Incident {
	now := time.Now()
	in := &store.Incident{
		DedupKey: ev.DedupKey, Source: ev.Source, Kind: string(ev.Kind), Channel: ev.Channel, ChannelID: c.cfg.ChannelFor(ev.Channel),
		Severity: string(ev.Severity), Status: "active", Version: 1,
		Title: ev.Title, Body: ev.Body, Host: ev.Host, Link: ev.Link, Labels: ev.Labels,
		DesiredPresent: true, Confirmed: false, CreatedAt: now, LastSeenFiring: now,
	}
	in.ContentHash = c.contentHash(in)
	return in
}

func (c *Core) applyFiring(in *store.Incident, ev event.Event) {
	in.Severity = string(ev.Severity)
	in.Title, in.Body, in.Host, in.Link, in.Labels = ev.Title, ev.Body, ev.Host, ev.Link, ev.Labels
	in.LastSeenFiring = time.Now()
	in.Heartbeat = true // a second firing proves a heartbeat; one-shots never reach here
	h := c.contentHash(in)
	if h != in.ContentHash || in.MessageID == "" {
		in.Confirmed = false
	}
	in.ContentHash = h
}

func markResolved(in *store.Incident) {
	now := time.Now()
	in.Status = "resolved"
	in.DesiredPresent = false
	in.Confirmed = false
	in.ResolvedAt = &now
}

func (c *Core) contentHash(in *store.Incident) string {
	return card.Build(in, c.cfg.Styles.Resolve(in.Source)).ContentHash()
}

func (c *Core) mutate(ctx context.Context, key string, fn func(*store.Incident)) error {
	release := c.coord.Lock(key)
	defer release()
	in, err := c.store.ActiveByKey(key)
	if err != nil || in == nil {
		return err // nil,nil => no-op
	}
	fn(in)
	h := c.contentHash(in)
	if h != in.ContentHash {
		in.Confirmed = false
	}
	in.ContentHash = h
	if err := c.store.Update(in); err != nil {
		return err
	}
	c.wake.Wake()
	return nil
}

func (c *Core) Acknowledge(ctx context.Context, key, user string) error {
	release := c.coord.Lock(key)
	defer release()
	in, err := c.store.ActiveByKey(key)
	if err != nil || in == nil {
		return err
	}
	if event.Kind(in.Kind).Behavior().AckCloses {
		markResolved(in) // a notify's ack dismisses it: the converger deletes the card
	} else {
		now := time.Now()
		in.AckedAt, in.AckedBy = &now, &user
		h := c.contentHash(in)
		if h != in.ContentHash {
			in.Confirmed = false
		}
		in.ContentHash = h
	}
	if err := c.store.Update(in); err != nil {
		return err
	}
	c.wake.Wake()
	return nil
}

func (c *Core) Snooze(ctx context.Context, key, user string, d time.Duration) error {
	return c.mutate(ctx, key, func(in *store.Incident) {
		until := time.Now().Add(d)
		in.SnoozedUntil, in.AckedBy = &until, &user
	})
}

func (c *Core) Resolve(ctx context.Context, key, user string) error {
	release := c.coord.Lock(key)
	defer release()
	in, err := c.store.ActiveByKey(key)
	if err != nil || in == nil {
		return err
	}
	markResolved(in)
	if err := c.store.Update(in); err != nil {
		return err
	}
	c.wake.Wake()
	return nil
}
