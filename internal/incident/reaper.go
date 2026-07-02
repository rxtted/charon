package incident

import (
	"context"
	"errors"
	"time"

	"github.com/rxtted/cheron/internal/config"
	"github.com/rxtted/cheron/internal/lock"
	"github.com/rxtted/cheron/internal/store"
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
		// re-verify under the lock: skip a row a concurrent resolve closed, or one
		// that is not heartbeat-backed. only a real close below wakes the converger.
		if err != nil || fresh == nil || !fresh.Heartbeat {
			release()
			continue
		}
		markResolved(fresh)
		err = r.store.Update(fresh)
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

// MarkUnconfirmed is a boot step: active incidents are re-checked against discord.
func (c *Core) MarkUnconfirmed(ctx context.Context) error {
	return c.store.MarkAllUnconfirmed()
}
