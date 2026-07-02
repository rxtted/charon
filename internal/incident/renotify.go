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

// repostBlocked reports whether an incident must not be reposted right now:
// acked, has no live message, or is still snoozed. evaluated under the key lock
// so a concurrent ack/snooze is respected even when the batch select predated it.
func repostBlocked(in *store.Incident, now time.Time) bool {
	return in.AckedAt != nil || in.MessageID == "" ||
		(in.SnoozedUntil != nil && in.SnoozedUntil.After(now))
}

func (r *Renotifier) Sweep(now time.Time) error {
	due, err := r.store.DueForRenotify(now, now.Add(-r.cfg.RenotifyEvery))
	if err != nil {
		return err
	}
	for _, in := range due {
		release := r.coord.Lock(in.DedupKey)
		fresh, err := r.store.ActiveByKey(in.DedupKey)
		// re-verify under the lock: a concurrent ack or snooze (same key lock)
		// could have landed since the unlocked batch select.
		if err != nil || fresh == nil || repostBlocked(fresh, now) {
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
