package event

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"sync/atomic"
	"time"
)

type Status string
type Severity string
type Kind string
type Action string

const (
	Firing   Status = "firing"
	Resolved Status = "resolved"

	Info     Severity = "info"
	Warning  Severity = "warning"
	Critical Severity = "critical"

	Alert  Kind = "alert"
	Notify Kind = "notify"

	Ack     Action = "ack"
	Snooze  Action = "snooze"
	Resolve Action = "resolve"
)

// behavior is what a kind does. add a kind to the table below, not a branch
// across the core, the sweeps, and the renderer.
type behavior struct {
	Dedups     bool     // repeat firings collapse onto the active card, else each fires a fresh one
	Renotifies bool     // eligible for the re-notify sweep
	AckCloses  bool     // ack deletes and closes the card, rather than muting and keeping it
	Actions    []Action // the lifecycle buttons the card shows, in order
}

var behaviors = map[Kind]behavior{
	Alert:  {Dedups: true, Renotifies: true, AckCloses: false, Actions: []Action{Ack, Snooze, Resolve}},
	Notify: {Dedups: false, Renotifies: false, AckCloses: true, Actions: []Action{Ack}},
}

func (k Kind) Behavior() behavior {
	b, ok := behaviors[k]
	if !ok {
		b = behaviors[Alert] // an unknown or empty kind is an alert
	}
	b.Actions = append([]Action(nil), b.Actions...) // hand out a copy, never the table's backing array
	return b
}

var notifySeq atomic.Uint64

// uniqueKey is a notify's charon-owned identity: notifies never dedup, so each
// one gets a fresh key rather than the stable derived one. wall-clock plus a
// process-lifetime counter is collision-free without any entropy read, the counter
// separates notifies minted in the same instant, so two can never collapse onto one card.
func uniqueKey() string {
	return "notify:" + strconv.FormatInt(time.Now().UnixNano(), 10) + "-" + strconv.FormatUint(notifySeq.Add(1), 10)
}

// Event is the one type the core sees. Every emitter and adapter produces it.
type Event struct {
	Source   string            `json:"source"`
	Kind     Kind              `json:"kind,omitempty"`
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
	if _, ok := behaviors[e.Kind]; !ok {
		e.Kind = Alert // empty or unknown kinds resolve to alert before anything reads the kind
	}
	if e.Severity == "" {
		e.Severity = Warning
	}
	switch {
	case !e.Kind.Behavior().Dedups:
		e.DedupKey = uniqueKey() // notifies never dedup; the key is charon-owned, not honoured from the emitter
	case e.DedupKey == "":
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
