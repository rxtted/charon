package servarr

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rxtted/charon/internal/event"
)

func match(t *testing.T, path, body string) []event.Event {
	t.Helper()
	r := httptest.NewRequest("POST", path, strings.NewReader(body))
	evs, err := New(appFor(path)).Match(r)
	if err != nil {
		t.Fatalf("Match(%s): %v", path, err)
	}
	return evs
}

func appFor(path string) app {
	for _, a := range apps {
		if a.path == path {
			return a
		}
	}
	panic("no app for " + path)
}

const healthBody = `{"level":"warning","message":"Indexers unavailable due to failures for more than 6 hours: My Indexer, Another Indexer","type":"IndexerStatusCheck","wikiUrl":"https://wiki.servarr.com/radarr/system#indexers-are-unavailable-due-to-failures","eventType":"Health","instanceName":"Radarr","applicationUrl":"https://radarr.example.com"}`

const healthRestoredBody = `{"level":"warning","message":"Indexers unavailable due to failures for more than 6 hours: My Indexer","type":"IndexerStatusCheck","wikiUrl":"https://wiki.servarr.com/radarr/system#x","eventType":"HealthRestored","instanceName":"Radarr"}`

func TestHealthFires(t *testing.T) {
	evs := match(t, "/radarr", healthBody)
	if len(evs) != 1 {
		t.Fatalf("want 1 event, got %d", len(evs))
	}
	e := evs[0]
	if e.Source != "radarr" || e.Channel != "media" || e.Status != event.Firing || e.Kind != event.Alert {
		t.Fatalf("bad routing: %+v", e)
	}
	if e.DedupKey != "radarr:health:IndexerStatusCheck" {
		t.Fatalf("dedup = %q", e.DedupKey)
	}
	if e.Severity != event.Warning || !strings.HasPrefix(e.Title, "Indexers unavailable") {
		t.Fatalf("bad content: %+v", e)
	}
	if e.Labels["check"] != "IndexerStatusCheck" || e.Labels["wiki"] == "" {
		t.Fatalf("labels = %v", e.Labels)
	}
	if e.Link != "https://radarr.example.com/system/status" {
		t.Fatalf("link = %q", e.Link)
	}
}

func TestHealthRestoredResolves(t *testing.T) {
	evs := match(t, "/radarr", healthRestoredBody)
	if len(evs) != 1 || evs[0].Status != event.Resolved {
		t.Fatalf("want one resolved event, got %+v", evs)
	}
	if evs[0].DedupKey != "radarr:health:IndexerStatusCheck" {
		t.Fatalf("resolve key = %q", evs[0].DedupKey)
	}
}

func TestFourInstancesRegistered(t *testing.T) {
	want := map[string]string{"/radarr": "radarr", "/sonarr": "sonarr", "/lidarr": "lidarr", "/prowlarr": "prowlarr"}
	got := map[string]string{}
	for _, a := range apps {
		got[a.path] = a.source
	}
	if len(got) != 4 {
		t.Fatalf("want 4 apps, got %d", len(got))
	}
	for p, s := range want {
		if got[p] != s {
			t.Fatalf("%s -> %q, want %q", p, got[p], s)
		}
	}
}

func TestDropsGrab(t *testing.T) {
	evs := match(t, "/radarr", `{"eventType":"Grab","instanceName":"Radarr"}`)
	if evs != nil {
		t.Fatalf("Grab should drop, got %+v", evs)
	}
}

func TestRejectsGarbage(t *testing.T) {
	r := httptest.NewRequest("POST", "/radarr", strings.NewReader("not json"))
	if _, err := New(apps[0]).Match(r); err == nil {
		t.Fatal("expected error on malformed body")
	}
}
