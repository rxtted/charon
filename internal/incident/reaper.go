package incident

import (
	"context"
	"errors"
	"time"

	"github.com/rotten-division/charon/internal/config"
	"github.com/rotten-division/charon/internal/lock"
	"github.com/rotten-division/charon/internal/store"
)

type Reaper struct {
	store *store.Store
	cfg   config.Config
	coord *lock.Keyed
	wake  Waker
}

func NewReaper(s *store.Store, cfg config.Config, coord *lock.Keyed, w Waker) *Reaper {
	return &Reaper{store: s, cfg: cfg, coord: coord, wake: w}
}

func (r *Reaper) Sweep(now time.Time) error {
	due, err := r.store.DueForReap(now.Add(-r.cfg.ReaperGrace))
	if err != nil {
		return err
	}
	for _, in := range due {
		release := r.coord.Lock(in.DedupKey)
		fresh, err := r.store.ActiveByKey(in.DedupKey)
		if err == nil && fresh != nil && fresh.Heartbeat {
			markResolved(fresh)
			err = r.store.Update(fresh)
		}
		release()
		if err != nil && !errors.Is(err, store.ErrStale) {
			return err
		}
		if err == nil {
			r.wake.Wake()
		}
	}
	return nil
}

// markUnconfirmed is a boot step: active incidents are re-checked against discord.
func (c *Core) MarkUnconfirmed(ctx context.Context) error {
	return c.store.MarkAllUnconfirmed()
}
