package incident

import (
	"errors"
	"time"

	"github.com/rotten-division/charon/internal/config"
	"github.com/rotten-division/charon/internal/lock"
	"github.com/rotten-division/charon/internal/store"
)

type Renotifier struct {
	store *store.Store
	cfg   config.Config
	coord *lock.Keyed
	wake  Waker
}

func NewRenotifier(s *store.Store, cfg config.Config, coord *lock.Keyed, w Waker) *Renotifier {
	return &Renotifier{store: s, cfg: cfg, coord: coord, wake: w}
}

func (r *Renotifier) Sweep(now time.Time) error {
	due, err := r.store.DueForRenotify(now, now.Add(-r.cfg.RenotifyEvery))
	if err != nil {
		return err
	}
	for _, in := range due {
		release := r.coord.Lock(in.DedupKey)
		fresh, err := r.store.ActiveByKey(in.DedupKey)
		if err != nil || fresh == nil || fresh.AckedAt != nil || fresh.MessageID == "" {
			release()
			continue
		}
		fresh.StaleMessageID = fresh.MessageID // the converger owns the delete-then-post
		fresh.MessageID = ""
		fresh.Confirmed = false
		fresh.SnoozedUntil = nil
		fresh.LastNotifiedAt = &now
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
