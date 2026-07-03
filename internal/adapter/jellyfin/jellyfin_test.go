package jellyfin

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rxtted/charon/internal/event"
)

func match(t *testing.T, body string) []event.Event {
	t.Helper()
	r := httptest.NewRequest("POST", "/jellyfin", strings.NewReader(body))
	evs, err := New().Match(r)
	if err != nil {
		t.Fatalf("Match: %v", err)
	}
	return evs
}

func TestLockoutIsCriticalAlert(t *testing.T) {
	e := match(t, `{"notificationType":"UserLockedOut","username":"noah","userId":"u1"}`)[0]
	if e.Kind != event.Alert || e.Channel != "infra" || e.Severity != event.Critical {
		t.Fatalf("bad routing: %+v", e)
	}
	if e.DedupKey != "jf:lockout:noah" || e.Title != "User locked out: noah" {
		t.Fatalf("key/title: %+v", e)
	}
}

func TestAuthFailureIsNotify(t *testing.T) {
	e := match(t, `{"notificationType":"AuthenticationFailure","username":"noah","deviceName":"Firefox","app":"Jellyfin Web","remoteEndPoint":"10.0.0.9"}`)[0]
	if e.Kind != event.Notify || e.Channel != "infra" {
		t.Fatalf("bad routing: %+v", e)
	}
	if e.Title != "Login failed for noah" {
		t.Fatalf("title = %q", e.Title)
	}
	if e.Labels["ip"] != "10.0.0.9" || e.Labels["device"] != "Firefox" || e.Labels["client"] != "Jellyfin Web" {
		t.Fatalf("labels = %v", e.Labels)
	}
}

func TestSubtitleFailureToMedia(t *testing.T) {
	e := match(t, `{"notificationType":"SubtitleDownloadFailure","seriesName":"The Wire","seasonNumber":"1","episodeNumber":"3","itemName":"The Buys"}`)[0]
	if e.Kind != event.Notify || e.Channel != "media" {
		t.Fatalf("bad routing: %+v", e)
	}
	if e.Title != "Subtitle download failed for The Wire S1E3" {
		t.Fatalf("title = %q", e.Title)
	}
}

func TestPluginFailureBody(t *testing.T) {
	e := match(t, `{"notificationType":"PluginInstallationFailed","pluginName":"Trakt","pluginVersion":"1.2","exceptionMessage":"network unreachable"}`)[0]
	if e.Channel != "infra" || e.Body != "network unreachable" || e.Title != "Plugin install failed: Trakt" {
		t.Fatalf("bad mapping: %+v", e)
	}
}

func TestEscapedExceptionDecodes(t *testing.T) {
	// the operator template json_encodes free text, so quotes/newlines/backslashes
	// arrive as valid JSON escapes; json.Unmarshal decodes them to the real message.
	body := `{"notificationType":"PluginInstallationFailed","pluginName":"Trakt","exceptionMessage":"line one\nsaid \"boom\" at C:\\plugins"}`
	e := match(t, body)[0]
	if e.Body != "line one\nsaid \"boom\" at C:\\plugins" {
		t.Fatalf("escaped exception not decoded: %q", e.Body)
	}
}

func TestPendingRestartOneShot(t *testing.T) {
	e := match(t, `{"notificationType":"PendingRestart"}`)[0]
	if e.Kind != event.Alert || e.DedupKey != "jf:pending-restart" {
		t.Fatalf("bad mapping: %+v", e)
	}
}

func TestDropsPlayback(t *testing.T) {
	if evs := match(t, `{"notificationType":"PlaybackStart","itemName":"x"}`); evs != nil {
		t.Fatalf("playback should drop, got %+v", evs)
	}
	if evs := match(t, `{"notificationType":"PluginInstallationCancelled","pluginName":"x"}`); evs != nil {
		t.Fatalf("a cancelled install is not a failure, got %+v", evs)
	}
}

func TestRejectsGarbage(t *testing.T) {
	r := httptest.NewRequest("POST", "/jellyfin", strings.NewReader("not json"))
	if _, err := New().Match(r); err == nil {
		t.Fatal("expected error on malformed body")
	}
}
