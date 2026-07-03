package truenas

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/rxtted/charon/internal/adapter"
	"github.com/rxtted/charon/internal/event"
)

type Adapter struct{}

func New() Adapter { return Adapter{} }

func init() { adapter.Register(New()) }

func (Adapter) Name() string { return "truenas" }
func (Adapter) Path() string { return "/truenas" }

// slackPayload is the whole envelope truenas SCALE's Slack alert service posts:
// a single markdown string. all structure lives inside it.
type slackPayload struct {
	Text string `json:"text"`
}

func (Adapter) Match(r *http.Request) ([]event.Event, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", adapter.ErrNotMatched, err)
	}
	var sp slackPayload
	if err := json.Unmarshal(body, &sp); err != nil {
		return nil, fmt.Errorf("%w: %w", adapter.ErrNotMatched, err)
	}
	if sp.Text == "" {
		return nil, fmt.Errorf("%w: empty text", adapter.ErrNotMatched)
	}
	host, newAlerts, cleared := parse(sp.Text)
	if host == "" {
		// no "TrueNAS @ <host>" line: this isn't a truenas payload. reject so a
		// format drift or mis-wire is a logged 400, not a silent 202 drop.
		return nil, fmt.Errorf("%w: unrecognized truenas text", adapter.ErrNotMatched)
	}
	var evs []event.Event
	for _, msg := range newAlerts {
		e := alertEvent(host, msg, event.Firing)
		e.Normalize()
		evs = append(evs, e)
	}
	for _, msg := range cleared {
		e := alertEvent(host, msg, event.Resolved)
		e.Normalize()
		evs = append(evs, e)
	}
	return evs, nil // a recognized payload with no new/cleared bullets (a test alert) -> 202, no card
}

var (
	hostRe    = regexp.MustCompile(`@ (\S.*?)\s*$`)
	bulletRe  = regexp.MustCompile(`^\s*\*\s+`)
	clearedRe = regexp.MustCompile(`cleared:\s*$`)
	wsRe      = regexp.MustCompile(`\s+`)
)

// parse pulls the hostname and two bullet sets: the New-alerts delta (fired) and
// the cleared section (resolved). the Current-alerts snapshot is ignored; firing
// from the delta means an alert fires once and never gains a heartbeat, so the
// reaper can't false-close it. a wrapped continuation line is appended to its bullet.
func parse(text string) (host string, newAlerts, cleared []string) {
	if m := hostRe.FindStringSubmatch(firstLine(text)); m != nil {
		host = m[1]
	}
	var cur *[]string
	for _, ln := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(ln)
		switch {
		case strings.HasPrefix(trimmed, "New alert"):
			cur = &newAlerts
		case trimmed == "Current alerts:":
			cur = nil // the snapshot is ignored (see the doc comment)
		case clearedRe.MatchString(trimmed):
			cur = &cleared
		case bulletRe.MatchString(ln):
			if cur != nil {
				*cur = append(*cur, strings.TrimSpace(bulletRe.ReplaceAllString(ln, "")))
			}
		case trimmed != "" && cur != nil && len(*cur) > 0:
			(*cur)[len(*cur)-1] += " " + trimmed // continuation of a wrapped bullet
		}
	}
	return host, newAlerts, cleared
}

func firstLine(text string) string {
	if i := strings.IndexByte(text, '\n'); i >= 0 {
		return text[:i]
	}
	return text
}

func alertEvent(host, msg string, status event.Status) event.Event {
	return event.Event{
		Source:   "truenas",
		Kind:     event.Alert,
		Channel:  "infra",
		Status:   status,
		Severity: severity(msg),
		DedupKey: dedupKey(host, msg),
		Title:    msg,
		Host:     host,
	}
}

var faultTokens = []string{"DEGRADED", "FAULTED", "UNAVAIL", "FAILED", "OFFLINE", "CRITICAL"}

func severity(msg string) event.Severity {
	up := strings.ToUpper(msg)
	for _, tok := range faultTokens {
		if strings.Contains(up, tok) {
			return event.Critical
		}
	}
	return event.Warning
}

// dedupKey is the only identity truenas gives us: the payload carries no alert id,
// so we hash the host and a normalized message. a message whose args change drifts
// the key and misses its resolve, falling back to manual resolve.
func dedupKey(host, msg string) string {
	sum := sha256.Sum256([]byte(host + "\x00" + normalize(msg)))
	return "truenas:" + hex.EncodeToString(sum[:8])
}

// normalize lowercases and collapses whitespace only. digits are preserved so
// "Disk 1 OFFLINE" and "Disk 2 OFFLINE" stay distinct keys; the cost is that a
// countdown message whose number changes drifts its key and falls back to manual
// resolve.
func normalize(msg string) string {
	return strings.TrimSpace(wsRe.ReplaceAllString(strings.ToLower(msg), " "))
}
