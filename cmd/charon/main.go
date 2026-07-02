package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/rxtted/cheron/internal/adapter"
	_ "github.com/rxtted/cheron/internal/adapter/native" // self-register
	"github.com/rxtted/cheron/internal/config"
	"github.com/rxtted/cheron/internal/discord"
	"github.com/rxtted/cheron/internal/event"
	"github.com/rxtted/cheron/internal/incident"
	"github.com/rxtted/cheron/internal/ingest"
	"github.com/rxtted/cheron/internal/lock"
	"github.com/rxtted/cheron/internal/metrics"
	"github.com/rxtted/cheron/internal/store"
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

	m := metrics.New()
	b, err := discord.NewBot(cfg, nil) // actions late-bound below
	if err != nil {
		st.Close()
		return err
	}
	q := discord.NewQueue(b.Sender(), 1100*time.Millisecond, 1) // about one create per 1.1s per channel
	coord := lock.New()                                         // one shared per-key lock for every mutator
	conv := discord.NewConverger(st, q, coord, m.ConvergeErrors)
	core := incident.New(st, cfg, coord, conv)
	b.SetActions(core) // core satisfies discord.Actions

	renot := incident.NewRenotifier(st, cfg, coord, conv)
	reap := incident.NewReaper(st, cfg, coord, conv)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := core.MarkUnconfirmed(ctx); err != nil {
		st.Close()
		return err
	}

	// boot orphan sweep: a crash between posting a card and writing its
	// message_id can leave a bot-authored card with no matching incident. this is
	// best-effort and never fails startup.
	if keep, err := st.ActiveMessageIDs(); err != nil {
		slog.Error("boot orphan sweep: load active message ids failed", "err", err)
	} else if err := b.SweepOrphans(ctx, cfg.AllChannelIDs(), keep); err != nil {
		slog.Error("boot orphan sweep failed", "err", err)
	}

	// wg tracks the converger and sweep-loop goroutines so shutdown can wait for
	// them to exit before the store and bot are closed out from under them.
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); conv.Run(ctx) }()
	go func() {
		defer wg.Done()
		sweepLoop(ctx, time.Minute, func(now time.Time) {
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
	}()

	sink := func(ctx context.Context, ev event.Event) error {
		m.IngestTotal.WithLabelValues(string(ev.Status)).Inc()
		return core.Handle(ctx, ev)
	}
	h := ingest.Handler(adapter.Registered(), cfg.IngestToken, cfg.MaxBodyBytes, sink)
	ingestSrv, err := serve(cfg.ListenAddr, h)
	if err != nil {
		stop() // cancel ctx first: conv.Run/sweepLoop only exit on ctx.Done, so wg.Wait would hang otherwise
		wg.Wait()
		st.Close()
		return err
	}
	metricsSrv, err := serve(cfg.MetricsAddr, m.Handler())
	if err != nil {
		shutdownHTTP(ingestSrv)
		stop()
		wg.Wait()
		st.Close()
		return err
	}

	conv.Wake() // initial reconcile on boot
	if err := b.Open(ctx); err != nil {
		shutdownHTTP(ingestSrv, metricsSrv)
		stop()
		wg.Wait()
		st.Close()
		return err
	}

	<-ctx.Done()

	// shutdown order matters: drain in-flight HTTP handlers first, then let the
	// converger and sweep loop finish their current pass, then close the bot,
	// then close the store last. nothing may touch the store or bot once closed.
	shutdownHTTP(ingestSrv, metricsSrv)
	wg.Wait()
	b.Close()
	st.Close()
	return nil
}

// serve binds synchronously so a failed bind surfaces as a fatal startup error
// while the gateway still opens. the caller owns the returned server's shutdown.
func serve(addr string, h http.Handler) (*http.Server, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("bind %s: %w", addr, err)
	}
	srv := &http.Server{Handler: h}
	go func() { _ = srv.Serve(ln) }()
	return srv, nil
}

func shutdownHTTP(servers ...*http.Server) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, srv := range servers {
		_ = srv.Shutdown(ctx)
	}
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
