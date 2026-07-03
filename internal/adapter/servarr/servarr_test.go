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

const manualBody = `{"movie":{"title":"The Matrix","year":1999},"downloadInfo":{"quality":"Bluray-1080p","size":12008442000},"downloadClient":"qBittorrent","downloadClientType":"qBittorrent","downloadId":"ABCD1234","downloadStatus":"Warning","downloadStatusMessages":[{"title":"The.Matrix.1999","messages":["Found archive file, might need to be extracted"]}],"release":{"releaseTitle":"The.Matrix.1999.1080p.BluRay.x264-GROUP","indexer":"My Indexer (Prowlarr)","size":12008442000},"eventType":"ManualInteractionRequired","instanceName":"Radarr","applicationUrl":"https://radarr.example.com"}`

const importBody = `{"movie":{"title":"The Matrix"},"isUpgrade":false,"downloadClient":"qBittorrent","downloadId":"ABCD1234","eventType":"Download","instanceName":"Radarr","applicationUrl":"https://radarr.example.com"}`

func TestManualInteractionFires(t *testing.T) {
	e := match(t, "/radarr", manualBody)[0]
	if e.Status != event.Firing || e.Kind != event.Alert || e.Channel != "media" {
		t.Fatalf("bad routing: %+v", e)
	}
	if e.DedupKey != "radarr:manual:ABCD1234" {
		t.Fatalf("dedup = %q", e.DedupKey)
	}
	if e.Title != "Manual interaction required: The Matrix" {
		t.Fatalf("title = %q", e.Title)
	}
	if !strings.Contains(e.Body, "Found archive file") {
		t.Fatalf("body = %q", e.Body)
	}
	if e.Labels["quality"] != "Bluray-1080p" || e.Labels["client"] != "qBittorrent" || e.Labels["size"] != "11.2 GB" {
		t.Fatalf("labels = %v", e.Labels)
	}
	if e.Labels["indexer"] != "My Indexer (Prowlarr)" || e.Labels["release"] == "" {
		t.Fatalf("labels missing release/indexer: %v", e.Labels)
	}
}

func TestDownloadResolvesManualKey(t *testing.T) {
	e := match(t, "/radarr", importBody)[0]
	if e.Status != event.Resolved || e.DedupKey != "radarr:manual:ABCD1234" {
		t.Fatalf("download should resolve the manual key: %+v", e)
	}
}

func TestManualDroppedOnProwlarr(t *testing.T) {
	if evs := match(t, "/prowlarr", manualBody); evs != nil {
		t.Fatalf("prowlarr has no manual-interaction, want drop, got %+v", evs)
	}
}

const dlFailBody = `{"eventType":"DownloadFailure","instanceName":"Lidarr","applicationUrl":"https://lidarr.example.com","quality":"FLAC","qualityVersion":1,"releaseTitle":"Radiohead - In Rainbows (2007) [FLAC-16bit]","downloadClient":"qBittorrent","downloadId":"LID1234"}`

const importFailBody = `{"eventType":"ImportFailure","instanceName":"Lidarr","artist":{"name":"Radiohead","mbId":"a74b"},"trackFiles":[{"quality":"FLAC","size":41231234}],"isUpgrade":false,"downloadClient":"qBittorrent","downloadId":"LID1234"}`

func TestLidarrDownloadFailure(t *testing.T) {
	e := match(t, "/lidarr", dlFailBody)[0]
	if e.Source != "lidarr" || e.Channel != "media" || e.Status != event.Firing || e.Kind != event.Alert {
		t.Fatalf("bad routing: %+v", e)
	}
	if e.DedupKey != "lidarr:import:LID1234" || e.Severity != event.Warning {
		t.Fatalf("dedup/sev: %+v", e)
	}
	if e.Title != "Download failed: Radiohead - In Rainbows (2007) [FLAC-16bit]" {
		t.Fatalf("title = %q", e.Title)
	}
	if e.Labels["quality"] != "FLAC" || e.Labels["release"] == "" {
		t.Fatalf("labels = %v", e.Labels)
	}
}

func TestLidarrImportFailure(t *testing.T) {
	e := match(t, "/lidarr", importFailBody)[0]
	if e.DedupKey != "lidarr:import:LID1234" {
		t.Fatalf("dedup = %q", e.DedupKey)
	}
	if e.Title != "Import failed: Radiohead" {
		t.Fatalf("title = %q", e.Title)
	}
	if e.Labels["artist"] != "Radiohead" || e.Labels["quality"] != "FLAC" {
		t.Fatalf("labels = %v", e.Labels)
	}
}

func TestLidarrDownloadDropped(t *testing.T) {
	// a lidarr import success (eventType Download) has no manual to resolve: drop.
	if evs := match(t, "/lidarr", `{"eventType":"Download","instanceName":"Lidarr","downloadId":"X"}`); evs != nil {
		t.Fatalf("lidarr Download should drop, got %+v", evs)
	}
}

func TestDropsTestEvent(t *testing.T) {
	// the arr Test button sends eventType "Test" with a body that need not fit the
	// mapping struct (here release is a string where a real event has an object).
	// it must drop to 202, not 400 on the shape. match() fatals if Match errors.
	if evs := match(t, "/radarr", `{"eventType":"Test","instanceName":"Radarr","release":"test string"}`); evs != nil {
		t.Fatalf("Test event should produce no card, got %+v", evs)
	}
}
