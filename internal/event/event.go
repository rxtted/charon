package event

import (
	"crypto/sha256"
	"encoding/hex"
	"time"
)

type Status string
type Severity string

const (
	Firing   Status = "firing"
	Resolved Status = "resolved"

	Info     Severity = "info"
	Warning  Severity = "warning"
	Critical Severity = "critical"
)

// Event is the one type the core sees. Every emitter and adapter produces it.
type Event struct {
	Source   string            `json:"source"`
	DedupKey string            `json:"dedup_key"`
	GroupKey string            `json:"group_key,omitempty"` // reserved; unused in v1
	Status   Status            `json:"status"`
	Severity Severity          `json:"severity"`
	Channel  string            `json:"channel"`
	Title    string            `json:"title"`
	Body     string            `json:"body,omitempty"`
	Host     string            `json:"host,omitempty"`
	Link     string            `json:"link,omitempty"`
	Labels   map[string]string `json:"labels,omitempty"`
	Time     time.Time         `json:"time"`
}

// Normalize fills the defaults the spec lets an emitter omit.
func (e *Event) Normalize() {
	if e.Severity == "" {
		e.Severity = Warning
	}
	if e.DedupKey == "" {
		e.DedupKey = DeriveKey(e.Source, e.Channel, e.Title)
	}
	if e.Time.IsZero() {
		e.Time = time.Now()
	}
}

// DeriveKey is the fallback identity when an emitter omits DedupKey. a title that
// reworks between firings changes the hash, so the incident splits; emitters that
// care about stable dedup should set DedupKey themselves.
func DeriveKey(source, channel, title string) string {
	sum := sha256.Sum256([]byte(source + "\x00" + channel + "\x00" + title))
	return "derived:" + hex.EncodeToString(sum[:8])
}
