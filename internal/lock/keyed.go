package lock

// entry is a per-key lock channel plus a refcount of holders/waiters, so the
// entry can be evicted from the map once nobody needs it anymore.
type entry struct {
	ch  chan struct{}
	ref int
}

// Keyed hands out a lock per key so mutations of one incident serialize.
type Keyed struct {
	guard chan struct{}
	m     map[string]*entry
}

func New() *Keyed {
	return &Keyed{guard: make(chan struct{}, 1), m: map[string]*entry{}}
}

// Lock blocks until the key is free and returns a release func to call when done.
// Keys are refcounted: the entry is created on first use and deleted from the map
// once its last holder releases, so the map doesn't grow without bound.
func (k *Keyed) Lock(key string) func() {
	k.guard <- struct{}{}
	e, ok := k.m[key]
	if !ok {
		e = &entry{ch: make(chan struct{}, 1)}
		k.m[key] = e
	}
	e.ref++
	<-k.guard

	e.ch <- struct{}{}
	return func() {
		<-e.ch
		k.guard <- struct{}{}
		e.ref--
		if e.ref == 0 {
			delete(k.m, key)
		}
		<-k.guard
	}
}
