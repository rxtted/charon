package lock

// Keyed hands out a lock per key so mutations of one incident serialize.
type Keyed struct {
	guard chan struct{}
	m     map[string]chan struct{}
}

func New() *Keyed {
	return &Keyed{guard: make(chan struct{}, 1), m: map[string]chan struct{}{}}
}

// Lock blocks until the key is free and returns a release func to call when done.
func (k *Keyed) Lock(key string) func() {
	k.guard <- struct{}{}
	ch, ok := k.m[key]
	if !ok {
		ch = make(chan struct{}, 1)
		k.m[key] = ch
	}
	<-k.guard
	ch <- struct{}{}
	return func() { <-ch }
}
