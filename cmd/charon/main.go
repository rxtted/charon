package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rotten-division/charon/internal/adapter"
	_ "github.com/rotten-division/charon/internal/adapter/native" // self-register
	"github.com/rotten-division/charon/internal/config"
	"github.com/rotten-division/charon/internal/discord"
	"github.com/rotten-division/charon/internal/event"
	"github.com/rotten-division/charon/internal/incident"
	"github.com/rotten-division/charon/internal/ingest"
	"github.com/rotten-division/charon/internal/lock"
	"github.com/rotten-division/charon/internal/metrics"
	"github.com/rotten-division/charon/internal/store"
)

func main() {
	if err := run(); err != nil {
		slog.Error("charon exited", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load(os.LookupEnv, os.Getenv("CHARON_CONFIG"))
	if err != nil {
		return err
	}
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer st.Close()

	m := metrics.New()
	b, err := discord.NewBot(cfg, nil) // actions late-bound below
	if err != nil {
		return err
	}
	q := discord.NewQueue(b.Sender(), 1100*time.Millisecond, 1) // about one create per 1.1s per channel
	coord := lock.New()                                         // one shared per-key lock for every mutator
	conv := discord.NewConverger(st, q, coord)
	core := incident.New(st, cfg, coord, conv)
	b.SetActions(core) // core satisfies discord.Actions

	renot := incident.NewRenotifier(st, cfg, coord, conv)
	reap := incident.NewReaper(st, cfg, coord, conv)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := core.MarkUnconfirmed(ctx); err != nil {
		return err
	}
	go conv.Run(ctx)
	go sweepLoop(ctx, time.Minute, func(now time.Time) {
		degraded := q.Degraded()
		m.Degraded.Set(boolToFloat(degraded))
		if n, err := st.CountActive(); err == nil {
			m.ActiveIncidents.Set(float64(n))
		}
		if !degraded { // pause re-notify while the discord path is down; the converger catches up on recovery
			_ = renot.Sweep(now)
		}
		_ = reap.Sweep(now) // the reaper only touches the store; safe while degraded
	})

	sink := func(ctx context.Context, ev event.Event) error {
		m.IngestTotal.WithLabelValues(string(ev.Status)).Inc()
		return core.Handle(ctx, ev)
	}
	h := ingest.Handler(adapter.Registered(), cfg.IngestToken, cfg.MaxBodyBytes, sink)
	if err := serve(ctx, cfg.ListenAddr, h); err != nil {
		return err
	}
	if err := serve(ctx, cfg.MetricsAddr, m.Handler()); err != nil {
		return err
	}

	conv.Wake() // initial reconcile on boot
	if err := b.Open(ctx); err != nil {
		return err
	}
	defer b.Close()
	<-ctx.Done()
	return nil
}

// serve binds synchronously so a failed bind is a fatal startup error, not a
// silent goroutine death while the gateway still opens.
func serve(ctx context.Context, addr string, h http.Handler) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("bind %s: %w", addr, err)
	}
	srv := &http.Server{Handler: h}
	go func() { _ = srv.Serve(ln) }()
	context.AfterFunc(ctx, func() { _ = srv.Shutdown(context.Background()) })
	return nil
}

func sweepLoop(ctx context.Context, every time.Duration, fn func(time.Time)) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			fn(now)
		}
	}
}

func boolToFloat(b bool) float64 {
	if b {
		return 1
	}
	return 0
}
