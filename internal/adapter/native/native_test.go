package native

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParsesSingle(t *testing.T) {
	body := `{"source":"grafana","dedup_key":"k","status":"firing","severity":"critical","channel":"infra","title":"host down"}`
	r := httptest.NewRequest("POST", "/ingest", strings.NewReader(body))
	a := New()
	evs, err := a.Match(r)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 || evs[0].Title != "host down" || evs[0].Severity != "critical" {
		t.Fatalf("parsed wrong: %+v", evs)
	}
	if a.Path() != "/ingest" {
		t.Fatalf("path = %q", a.Path())
	}
}

func TestParsesBatch(t *testing.T) {
	body := `[{"status":"firing","channel":"infra","title":"a"},{"status":"firing","channel":"apps","title":"b"}]`
	r := httptest.NewRequest("POST", "/ingest", strings.NewReader(body))
	evs, err := New().Match(r)
	if err != nil || len(evs) != 2 {
		t.Fatalf("batch not expanded: %d %v", len(evs), err)
	}
}

func TestRejectsGarbage(t *testing.T) {
	r := httptest.NewRequest("POST", "/ingest", strings.NewReader("not json"))
	if _, err := New().Match(r); err == nil {
		t.Fatal("expected error on malformed body")
	}
}
