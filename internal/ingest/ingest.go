package ingest

import (
	"context"
	"crypto/subtle"
	"errors"
	"log/slog"
	"net/http"

	"github.com/rxtted/cheron/internal/adapter"
	"github.com/rxtted/cheron/internal/event"
)

func Handler(adapters []adapter.Adapter, token string, maxBody int64, sink func(context.Context, event.Event) error) http.Handler {
	mux := http.NewServeMux()
	for _, a := range adapters {
		a := a
		mux.HandleFunc("POST "+a.Path(), func(w http.ResponseWriter, r *http.Request) {
			if token != "" && !authorized(r, token) {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, maxBody)
			evs, err := a.Match(r)
			if err != nil {
				var mbe *http.MaxBytesError
				if errors.As(err, &mbe) {
					http.Error(w, "payload too large", http.StatusRequestEntityTooLarge)
					return
				}
				slog.Warn("ingest rejected", "adapter", a.Name(), "err", err)
				http.Error(w, "unrecognized payload", http.StatusBadRequest)
				return
			}
			for _, ev := range evs { // a batch (grafana) fans to one core call each
				if err := sink(r.Context(), ev); err != nil {
					slog.Error("ingest sink failed", "err", err)
					http.Error(w, "internal", http.StatusInternalServerError)
					return
				}
			}
			w.WriteHeader(http.StatusAccepted)
		})
	}
	return mux
}

func authorized(r *http.Request, token string) bool {
	const p = "Bearer "
	h := r.Header.Get("Authorization")
	return len(h) > len(p) && subtleEqual(h[len(p):], token)
}

func subtleEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
