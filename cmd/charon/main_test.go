package main

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/rxtted/charon/internal/adapter"
)

// TestAdaptersRegistered guards the blank imports in main.go: the ingest tests
// use native.New() directly and never load them, so only a package-main test
// catches a missing one.
func TestAdaptersRegistered(t *testing.T) {
	paths := map[string]bool{}
	for _, a := range adapter.Registered() {
		paths[a.Path()] = true
	}
	for _, want := range []string{"/ingest", "/radarr", "/sonarr", "/lidarr", "/prowlarr", "/truenas", "/jellyfin"} {
		if !paths[want] {
			t.Fatalf("adapter path %q not registered; check the blank import in main.go", want)
		}
	}
}

// sweepLoop is one of the two
// goroutines run()'s wg.Wait() joins on shutdown. it must return as soon as ctx
// is cancelled rather than waiting for its next tick.
func TestSweepStopsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		sweepLoop(ctx, time.Hour, func(time.Time) {}) // a tick this far out would never fire on its own
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("sweepLoop did not return promptly after context cancellation")
	}
}

// a boot deadlock this guards against: every early-return error path in run() joins the
// converger and sweep-loop goroutines with wg.Wait(), but those goroutines only
// exit on ctx.Done(). if an error path called wg.Wait() without first cancelling
// ctx (as run() briefly did), the process would hang forever on an ordinary
// startup failure like a bind conflict instead of fast-failing. this mirrors
// run()'s exact goroutine shape and asserts the fixed order (cancel, then wait)
// completes quickly.
func TestShutdownCancelsContext(t *testing.T) {
	ctx, stop := context.WithCancel(context.Background())
	defer stop()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); <-ctx.Done() }() // stands in for conv.Run(ctx)
	go func() { defer wg.Done(); sweepLoop(ctx, time.Hour, func(time.Time) {}) }()

	// this is the exact sequence an error path in run() must follow: cancel
	// before wg.Wait(), never the other way around.
	stop()
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("wg.Wait() hung: ctx must be cancelled before waiting on the goroutines it gates")
	}
}
