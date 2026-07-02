package ingest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rxttd/cheron/internal/adapter"
	"github.com/rxttd/cheron/internal/adapter/native"
	"github.com/rxttd/cheron/internal/event"
)

type adapterList = adapter.Adapter

func TestIngestRoutesAndAccepts(t *testing.T) {
	var got event.Event
	h := Handler([]adapterList{native.New()}, "", 1<<20, func(_ context.Context, e event.Event) error { got = e; return nil })
	body := `{"source":"grafana","status":"firing","channel":"infra","title":"host down"}`
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("POST", "/ingest", strings.NewReader(body)))
	if rr.Code != http.StatusAccepted {
		t.Fatalf("code = %d", rr.Code)
	}
	if got.Title != "host down" {
		t.Fatalf("sink not called: %+v", got)
	}
}

func TestIngestTokenRequiredWhenSet(t *testing.T) {
	h := Handler([]adapterList{native.New()}, "secret", 1<<20, func(context.Context, event.Event) error { return nil })
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("POST", "/ingest", strings.NewReader("{}")))
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("code = %d, want 401", rr.Code)
	}
}

// TestIngestOversizedBodyReturns413: a body over MaxBodyBytes must
// report 413. that requires the adapter's wrapped read error to still chain to
// *http.MaxBytesError so ingest's errors.As can see it.
func TestIngestOversizedBodyReturns413(t *testing.T) {
	h := Handler([]adapterList{native.New()}, "", 16, func(context.Context, event.Event) error { return nil })
	body := strings.Repeat("a", 1<<10)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("POST", "/ingest", strings.NewReader(body)))
	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("code = %d, want 413", rr.Code)
	}
}
