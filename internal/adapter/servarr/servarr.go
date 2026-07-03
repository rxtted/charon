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
	// read the event type first and drop anything we don't map (a grab, the arr's
	// built-in test button, and so on) before the full parse. that test payload
	// doesn't fit the mapping struct, and an event we'd discard shouldn't 400 on it.
	var head struct {
		EventType string `json:"eventType"`
	}
	if err := json.Unmarshal(body, &head); err != nil {
		return nil, fmt.Errorf("%w: %w", adapter.ErrNotMatched, err)
	}
	if head.EventType == "" {
		return nil, fmt.Errorf("%w: no eventType", adapter.ErrNotMatched)
	}
	if !mappable(head.EventType) {
		return nil, nil // recognized source, unmapped event: 202, no card
	}
	var p payload
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, fmt.Errorf("%w: %w", adapter.ErrNotMatched, err)
	}
	ev, ok := a.mapEvent(p)
	if !ok {
		return nil, nil // an app that lacks this event (a Download on prowlarr)
	}
	ev.Normalize()
	return []event.Event{ev}, nil
}

// mappable is the coarse gate over event types the adapter ever maps, checked
// before the full parse so an unmapped payload (the Test button) can't 400 on a
// shape it never needed to fit. per-app routing stays in mapEvent.
func mappable(eventType string) bool {
	switch eventType {
	case "Health", "HealthRestored", "ManualInteractionRequired", "Download", "DownloadFailure", "ImportFailure":
		return true
	default:
		return false
	}
}

func (a Adapter) mapEvent(p payload) (event.Event, bool) {
	switch p.EventType {
	case "Health":
		return a.health(p, event.Firing), true
	case "HealthRestored":
		return a.health(p, event.Resolved), true
	case "ManualInteractionRequired":
		if !a.app.hasManual {
			return event.Event{}, false
		}
		return a.manual(p), true
	case "Download":
		if !a.app.hasManual {
			return event.Event{}, false // lidarr/prowlarr import success: nothing to resolve
		}
		return a.downloadResolve(p), true
	case "DownloadFailure":
		if !a.app.isLidarr {
			return event.Event{}, false
		}
		return a.lidarrDownloadFailure(p), true
	case "ImportFailure":
		if !a.app.isLidarr {
			return event.Event{}, false
		}
		return a.lidarrImportFailure(p), true
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

func (a Adapter) manual(p payload) event.Event {
	labels := map[string]string{}
	if p.Release != nil {
		putIf(labels, "release", p.Release.ReleaseTitle)
		putIf(labels, "indexer", p.Release.Indexer)
	}
	if p.DownloadInfo != nil {
		putIf(labels, "quality", p.DownloadInfo.Quality)
		putIf(labels, "size", humanizeBytes(p.DownloadInfo.Size))
	}
	putIf(labels, "client", p.DownloadClient)
	putIf(labels, "status", p.DownloadStatus)
	return event.Event{
		Source:   a.app.source,
		Kind:     event.Alert,
		Channel:  "media",
		Status:   event.Firing,
		DedupKey: a.app.source + ":manual:" + p.DownloadID,
		Severity: event.Warning,
		Title:    "Manual interaction required: " + mediaTitle(p),
		Body:     statusBody(p.DownloadStatusMessages),
		Link:     appLink(p, "/activity/queue"),
		Labels:   labels,
	}
}

// downloadResolve clears a prior manual-interaction on the same downloadId. it is
// emitted for every radarr/sonarr import; the core no-ops it when no manual card
// is active, so a plain successful import stays cardless.
func (a Adapter) downloadResolve(p payload) event.Event {
	return event.Event{
		Source:   a.app.source,
		Kind:     event.Alert,
		Channel:  "media",
		Status:   event.Resolved,
		DedupKey: a.app.source + ":manual:" + p.DownloadID,
	}
}

func (a Adapter) lidarrDownloadFailure(p payload) event.Event {
	labels := map[string]string{}
	putIf(labels, "release", p.ReleaseTitle)
	putIf(labels, "quality", p.Quality)
	putIf(labels, "client", p.DownloadClient)
	return event.Event{
		Source:   a.app.source,
		Kind:     event.Alert,
		Channel:  "media",
		Status:   event.Firing,
		DedupKey: a.app.source + ":import:" + p.DownloadID,
		Severity: event.Warning,
		Title:    "Download failed: " + p.ReleaseTitle,
		Labels:   labels,
	}
}

func (a Adapter) lidarrImportFailure(p payload) event.Event {
	artist := ""
	if p.Artist != nil {
		artist = p.Artist.Name
	}
	labels := map[string]string{}
	putIf(labels, "artist", artist)
	if len(p.TrackFiles) > 0 {
		putIf(labels, "quality", p.TrackFiles[0].Quality)
	}
	putIf(labels, "client", p.DownloadClient)
	return event.Event{
		Source:   a.app.source,
		Kind:     event.Alert,
		Channel:  "media",
		Status:   event.Firing,
		DedupKey: a.app.source + ":import:" + p.DownloadID,
		Severity: event.Warning,
		Title:    "Import failed: " + artist,
		Labels:   labels,
	}
}

func mediaTitle(p payload) string {
	switch {
	case p.Movie != nil:
		return p.Movie.Title
	case p.Series != nil:
		return p.Series.Title
	case p.Artist != nil:
		return p.Artist.Name
	}
	return ""
}

func statusBody(msgs []statusMsg) string {
	var parts []string
	for _, m := range msgs {
		parts = append(parts, m.Messages...)
	}
	return strings.Join(parts, "; ")
}

func appLink(p payload, suffix string) string {
	if p.ApplicationURL == "" {
		return ""
	}
	return strings.TrimRight(p.ApplicationURL, "/") + suffix
}

// humanizeBytes renders a byte count as "11.2 GB", matching the repo's small
// local-formatter idiom rather than pulling a dependency.
func humanizeBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

func putIf(m map[string]string, k, v string) {
	if v != "" {
		m[k] = v
	}
}

