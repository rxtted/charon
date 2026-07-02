package discord

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/disgoorg/disgo/rest"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rxtted/charon/internal/card"
	"github.com/rxtted/charon/internal/lock"
	"github.com/rxtted/charon/internal/store"
)

// reconcileTimeout bounds a single reconcile's discord calls so a stuck REST
// request can't hold the row's key lock forever.
const reconcileTimeout = 15 * time.Second

type Converger struct {
	store  *store.Store
	q      *Queue
	coord  *lock.Keyed
	wake   chan struct{}
	errs   prometheus.Counter
	styles card.Set
	wrap   int
}

func NewConverger(s *store.Store, q *Queue, coord *lock.Keyed, errs prometheus.Counter, styles card.Set, wrap int) *Converger {
	return &Converger{store: s, q: q, coord: coord, wake: make(chan struct{}, 1), errs: errs, styles: styles, wrap: wrap}
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
		c.pass(ctx)
	}
}

// pass reconciles every row that needs it. one row's error doesn't starve the
// rest of the sweep: it's logged and the loop moves on, unless the queue reports
// a systemic outage, in which case retrying every remaining row is pointless. it
// also checks ctx between rows so a shutdown aborts the remaining rows promptly
// instead of running each one out to its own timeout.
func (c *Converger) pass(ctx context.Context) {
	rows, err := c.store.NeedingConverge()
	if err != nil {
		slog.Error("converge query failed", "err", err)
		return
	}
	for _, in := range rows {
		if ctx.Err() != nil {
			return // shutting down: leave the rest for next boot's reconcile
		}
		if err := c.reconcile(ctx, in); err != nil {
			c.errs.Inc()
			slog.Warn("reconcile failed, will retry", "key", in.DedupKey, "err", err)
			if c.q.Degraded() {
				return // discord likely degraded; leave the rest for the next pass
			}
		}
	}
}

// reconcile holds the shared key lock for the whole row, so the discord call and
// the write that records it are atomic against the core and the sweeps. it reloads
// the row by id under the lock since dedup_key is no longer unique across rows.
func (c *Converger) reconcile(ctx context.Context, in *store.Incident) error {
	ctx, cancel := context.WithTimeout(ctx, reconcileTimeout)
	defer cancel()

	release := c.coord.Lock(in.DedupKey)
	defer release()
	in, err := c.store.ByID(in.ID) // any status: a resolved row still owes a delete
	if err != nil || in == nil {
		return err
	}
	if in.StaleMessageID != "" { // durable repost: clear the old card first
		if err := c.q.DeleteMsg(ctx, in.ChannelID, in.StaleMessageID); err != nil && !isNotFound(err) {
			return err
		}
		in.StaleMessageID = "" // gone either way: we deleted it, or discord already had
	}
	switch {
	case in.DesiredPresent && in.MessageID == "":
		r := card.Build(in, c.styles.Resolve(in.Source))
		id, err := c.q.Post(ctx, in.ChannelID, RenderCreate(r, c.wrap))
		if err != nil {
			return err
		}
		now := time.Now()
		in.MessageID, in.Confirmed, in.LastNotifiedAt = id, true, &now
	case in.DesiredPresent && !in.Confirmed:
		r := card.Build(in, c.styles.Resolve(in.Source))
		if err := c.q.Edit(ctx, in.ChannelID, in.MessageID, RenderUpdate(r, c.wrap)); err != nil {
			if !isNotFound(err) {
				return err
			}
			// the card vanished out from under us: clear it so the next pass reposts
			in.MessageID, in.Confirmed = "", false
		} else {
			in.Confirmed = true
		}
	case !in.DesiredPresent && in.MessageID != "":
		if err := c.q.DeleteMsg(ctx, in.ChannelID, in.MessageID); err != nil && !isNotFound(err) {
			return err
		}
		in.MessageID, in.Confirmed = "", true
	}
	return c.store.Update(in) // holds the lock, so the version precondition cannot lose here
}

// isNotFound reports whether err is a disgo REST error for a message that's
// already gone: either a plain 404, or discord's own "unknown message" code.
func isNotFound(err error) bool {
	var restErr *rest.Error
	if !errors.As(err, &restErr) {
		return false
	}
	if restErr.Code == rest.JSONErrorCodeUnknownMessage {
		return true
	}
	return restErr.Response != nil && restErr.Response.StatusCode == http.StatusNotFound
}
