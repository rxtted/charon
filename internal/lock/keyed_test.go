package lock

import (
	"fmt"
	"testing"
)

func (k *Keyed) len() int {
	k.guard <- struct{}{}
	n := len(k.m)
	<-k.guard
	return n
}

func TestEvictsReleasedKeys(t *testing.T) {
	k := New()
	for i := 0; i < 50; i++ {
		release := k.Lock(fmt.Sprintf("key-%d", i))
		release()
	}
	if n := k.len(); n != 0 {
		t.Fatalf("expected the map to be empty after every release, got %d entries", n)
	}
}

func TestConcurrentEviction(t *testing.T) {
	k := New()
	release1 := k.Lock("shared")
	done := make(chan func())
	go func() { done <- k.Lock("shared") }()

	release1() // hand off to the waiter
	release2 := <-done
	release2()

	if n := k.len(); n != 0 {
		t.Fatalf("expected the map to be empty once both holders released, got %d entries", n)
	}
}
