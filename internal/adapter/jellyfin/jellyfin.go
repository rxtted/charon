package jellyfin

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/rxtted/charon/internal/adapter"
	"github.com/rxtted/charon/internal/event"
)

type Adapter struct{}

func New() Adapter { return Adapter{} }

func init() { adapter.Register(New()) }

func (Adapter) Name() string { return "jellyfin" }
func (Adapter) Path() string { return "/jellyfin" }

// payload is the JSON contract this adapter defines; the operator's Generic
// destination template emits exactly these keys. every field is optional and each
// notificationType populates only its subset.
type payload struct {
	NotificationType string `json:"notificationType"`
	ServerName       string `json:"serverName"`
	ServerURL        string `json:"serverUrl"`
	Username         string `json:"username"`
	UserID           string `json:"userId"`
	App              string `json:"app"`
	DeviceName       string `json:"deviceName"`
	RemoteEndPoint   string `json:"remoteEndPoint"`
	ItemID           string `json:"itemId"`
	ItemType         string `json:"itemType"`
	ItemName         string `json:"itemName"`
	SeriesName       string `json:"seriesName"`
	SeasonNumber     string `json:"seasonNumber"`
	EpisodeNumber    string `json:"episodeNumber"`
	PluginName       string `json:"pluginName"`
	PluginVersion    string `json:"pluginVersion"`
	ExceptionMessage string `json:"exceptionMessage"`
}

// Match parses leniently: the plugin's Generic destination defaults to a
// text/plain Content-Type, so we don't require application/json.
func (Adapter) Match(r *http.Request) ([]event.Event, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", adapter.ErrNotMatched, err)
	}
	var p payload
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, fmt.Errorf("%w: %w", adapter.ErrNotMatched, err)
	}
	if p.NotificationType == "" {
		return nil, fmt.Errorf("%w: no notificationType", adapter.ErrNotMatched)
	}
	ev, ok := mapEvent(p)
	if !ok {
		return nil, nil // a playback/success/unhandled type: dropped
	}
	ev.Normalize()
	return []event.Event{ev}, nil
}

func mapEvent(p payload) (event.Event, bool) {
	switch p.NotificationType {
	case "UserLockedOut":
		e := base(event.Alert, "infra", event.Critical, "jf:lockout:"+p.Username, "User locked out: "+p.Username)
		return e, true
	case "PendingRestart":
		e := base(event.Alert, "infra", event.Warning, "jf:pending-restart", "Jellyfin restart pending")
		return e, true
	case "AuthenticationFailure":
		e := base(event.Notify, "infra", event.Warning, "", "Login failed for "+p.Username)
		e.Labels = putAll("user", p.Username, "device", p.DeviceName, "client", p.App, "ip", p.RemoteEndPoint)
		return e, true
	case "SubtitleDownloadFailure":
		e := base(event.Notify, "media", event.Info, "", "Subtitle download failed for "+itemLabel(p))
		e.Labels = putAll("item", p.ItemName, "series", p.SeriesName, "season", p.SeasonNumber, "episode", p.EpisodeNumber)
		return e, true
	case "PluginInstallationFailed":
		e := base(event.Notify, "infra", event.Warning, "", "Plugin install failed: "+p.PluginName)
		e.Body = p.ExceptionMessage
		e.Labels = putAll("plugin", p.PluginName, "version", p.PluginVersion)
		return e, true
	default:
		return event.Event{}, false
	}
}

func base(kind event.Kind, channel string, sev event.Severity, key, title string) event.Event {
	return event.Event{
		Source:   "jellyfin",
		Kind:     kind,
		Channel:  channel,
		Status:   event.Firing,
		Severity: sev,
		DedupKey: key,
		Title:    title,
	}
}

func itemLabel(p payload) string {
	if p.SeriesName != "" {
		if p.SeasonNumber != "" && p.EpisodeNumber != "" {
			return p.SeriesName + " S" + p.SeasonNumber + "E" + p.EpisodeNumber
		}
		return p.SeriesName
	}
	return p.ItemName
}

func putAll(kv ...string) map[string]string {
	m := map[string]string{}
	for i := 0; i+1 < len(kv); i += 2 {
		if kv[i+1] != "" {
			m[kv[i]] = kv[i+1]
		}
	}
	return m
}
