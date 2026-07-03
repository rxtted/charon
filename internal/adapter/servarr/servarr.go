package servarr

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/rxtted/charon/internal/adapter"
	"github.com/rxtted/charon/internal/event"
)

// app is the one owner of per-instance variation across the four arr apps that
// share the Connect webhook schema.
type app struct {
	source    string
	path      string
	hasManual bool // radarr, sonarr expose ManualInteractionRequired
	isLidarr  bool // lidarr adds DownloadFailure / ImportFailure
}

var apps = []app{
	{source: "radarr", path: "/radarr", hasManual: true},
	{source: "sonarr", path: "/sonarr", hasManual: true},
	{source: "lidarr", path: "/lidarr", isLidarr: true},
	{source: "prowlarr", path: "/prowlarr"},
}

type Adapter struct{ app app }

func New(a app) Adapter { return Adapter{app: a} }

func init() {
	for _, a := range apps {
		adapter.Register(New(a))
	}
}

func (a Adapter) Name() string { return a.app.source }
func (a Adapter) Path() string { return a.app.path }

func (a Adapter) Match(r *http.Request) ([]event.Event, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", adapter.ErrNotMatched, err)
	}
	var p payload
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, fmt.Errorf("%w: %w", adapter.ErrNotMatched, err)
	}
	if p.EventType == "" {
		return nil, fmt.Errorf("%w: no eventType", adapter.ErrNotMatched)
	}
	ev, ok := a.mapEvent(p)
	if !ok {
		return nil, nil // recognized but not surfaced (a success, or an app that lacks this event)
	}
	ev.Normalize()
	return []event.Event{ev}, nil
}

func (a Adapter) mapEvent(p payload) (event.Event, bool) {
	switch p.EventType {
	case "Health":
		return a.health(p, event.Firing), true
	case "HealthRestored":
		return a.health(p, event.Resolved), true
	default:
		return event.Event{}, false
	}
}

func (a Adapter) health(p payload, status event.Status) event.Event {
	labels := map[string]string{}
	putIf(labels, "check", p.Type)
	putIf(labels, "level", p.Level)
	putIf(labels, "wiki", p.WikiURL)
	return event.Event{
		Source:   a.app.source,
		Kind:     event.Alert,
		Channel:  "media",
		Status:   status,
		DedupKey: a.app.source + ":health:" + p.Type,
		Severity: healthSeverity(p.Level),
		Title:    p.Message,
		Link:     healthLink(p),
		Labels:   labels,
	}
}

func healthSeverity(level string) event.Severity {
	switch level {
	case "error":
		return event.Critical
	case "notice":
		return event.Info
	default: // warning, and any unexpected value
		return event.Warning
	}
}

func healthLink(p payload) string {
	if p.ApplicationURL != "" {
		return strings.TrimRight(p.ApplicationURL, "/") + "/system/status"
	}
	return p.WikiURL // applicationUrl is absent on HealthRestored and often empty
}

func putIf(m map[string]string, k, v string) {
	if v != "" {
		m[k] = v
	}
}
