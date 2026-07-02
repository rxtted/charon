package incident

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"github.com/rxtted/cheron/internal/config"
	"github.com/rxtted/cheron/internal/event"
	"github.com/rxtted/cheron/internal/lock"
	"github.com/rxtted/cheron/internal/store"
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
			applyFiring(cur, ev)
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
		DedupKey: ev.DedupKey, Channel: ev.Channel, ChannelID: c.cfg.ChannelFor(ev.Channel),
		Severity: string(ev.Severity), Status: "active", Version: 1,
		Title: ev.Title, Body: ev.Body, Host: ev.Host, Link: ev.Link, Labels: ev.Labels,
		DesiredPresent: true, Confirmed: false, CreatedAt: now, LastSeenFiring: now,
	}
	in.ContentHash = displayHash(in)
	return in
}

func applyFiring(in *store.Incident, ev event.Event) {
	in.Severity = string(ev.Severity)
	in.Title, in.Body, in.Host, in.Link, in.Labels = ev.Title, ev.Body, ev.Host, ev.Link, ev.Labels
	in.LastSeenFiring = time.Now()
	in.Heartbeat = true // a second firing proves a heartbeat; one-shots never reach here
	h := displayHash(in)
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

func displayHash(in *store.Incident) string {
	ack := ""
	if in.AckedAt != nil {
		ack = "acked:" + derefStr(in.AckedBy)
	}
	snz := ""
	if in.SnoozedUntil != nil {
		snz = in.SnoozedUntil.UTC().Format(time.RFC3339)
	}
	lbl, _ := json.Marshal(in.Labels)
	sum := sha256.Sum256([]byte(strings.Join(
		[]string{in.Severity, in.Title, in.Body, in.Host, ack, snz, string(lbl)}, "\x00")))
	return hex.EncodeToString(sum[:8])
}

func derefStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func (c *Core) mutate(ctx context.Context, key string, fn func(*store.Incident)) error {
	release := c.coord.Lock(key)
	defer release()
	in, err := c.store.ActiveByKey(key)
	if err != nil || in == nil {
		return err // nil,nil => no-op
	}
	fn(in)
	h := displayHash(in)
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
	return c.mutate(ctx, key, func(in *store.Incident) {
		now := time.Now()
		in.AckedAt, in.AckedBy = &now, &user
	})
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
