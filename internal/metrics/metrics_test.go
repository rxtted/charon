package metrics

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandlerServesMetrics(t *testing.T) {
	m := New()
	m.IngestTotal.WithLabelValues("firing").Inc()
	rr := httptest.NewRecorder()
	m.Handler().ServeHTTP(rr, httptest.NewRequest("GET", "/metrics", nil))
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), "charon_ingest_total") {
		t.Fatalf("metrics not served: %d", rr.Code)
	}
}
