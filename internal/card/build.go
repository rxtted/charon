package card

import (
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/rxtted/charon/internal/store"
)

type Rendered struct {
	Title       string
	Description string // raw, pre-wrap
	Severity    string // title-cased lead for the glance and colour
	Glance      []GlanceItem
	Data        []DataItem
	Footer      []string
	Note        string // ack/snooze line without the "-# " prefix, or ""
	Links       []Link
	Icon        string
	Accent      int
	Wrap        int // per-sender body wrap column; 0 uses the global default
	DedupKey    string
	CreatedAt   time.Time
}

var placeholder = regexp.MustCompile(`\{([a-z]+(?:\.[A-Za-z0-9_]+)?)\}`)

var builtinAccent = map[string]int{
	"critical": 0xE74C3C,
	"warning":  0xE67E22,
	"info":     0x3498DB,
	"resolved": 0x2ECC71,
}

const mutedAccent = 0x95A5A6

func Build(in *store.Incident, st Style) Rendered {
	ex := expander(in)
	title, _ := ex(st.Title)
	r := Rendered{
		Title:     title,
		Severity:  titleCase(in.Severity),
		Icon:      st.Icon,
		Accent:    accentFor(in, st),
		Note:      note(in),
		DedupKey:  in.DedupKey,
		CreatedAt: in.CreatedAt,
	}
	if st.Wrap != nil {
		r.Wrap = *st.Wrap
	}
	if st.Description != nil {
		if v, ok := ex(*st.Description); ok {
			r.Description = v
		}
	}
	for _, g := range st.Glance {
		if v, ok := ex(g.Value); ok {
			r.Glance = append(r.Glance, GlanceItem{Value: v, Code: g.Code})
		}
	}
	for _, d := range st.Data {
		if v, ok := ex(d.Value); ok {
			r.Data = append(r.Data, DataItem{Label: d.Label, Value: v})
		}
	}
	for _, f := range st.Footer {
		if v, ok := ex(f); ok {
			r.Footer = append(r.Footer, v)
		}
	}
	for _, l := range st.Links {
		u, ok := ex(l.URL)
		if !ok {
			continue
		}
		if !isHTTPURL(u) {
			slog.Warn("dropping link with non-http url", "source", in.Source, "url", u)
			continue
		}
		r.Links = append(r.Links, Link{Label: l.Label, URL: u})
	}
	return r
}

// expander replaces the closed placeholder set in a string. ok is false when any
// placeholder resolved empty, so the caller drops the whole item: an optional
// {labels.*} the event omits takes its glance/data/footer/link line with it rather
// than leaving a dangling label. {duration} stays a literal token for the render
// layer, which expands it live without touching the hash.
func expander(in *store.Incident) func(string) (string, bool) {
	base := map[string]string{
		"title":    in.Title,
		"body":     in.Body,
		"source":   in.Source,
		"host":     in.Host,
		"severity": titleCase(in.Severity),
		"link":     in.Link,
		"time":     in.CreatedAt.Format("15:04"),
	}
	return func(s string) (string, bool) {
		ok := true
		out := placeholder.ReplaceAllStringFunc(s, func(m string) string {
			key := m[1 : len(m)-1]
			if key == "duration" {
				return m
			}
			var v string
			if strings.HasPrefix(key, "labels.") {
				v = in.Labels[strings.TrimPrefix(key, "labels.")]
			} else {
				v = base[key]
			}
			if v == "" {
				ok = false
			}
			return v
		})
		return out, ok
	}
}

func isHTTPURL(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

func accentFor(in *store.Incident, st Style) int {
	if in.AckedAt != nil {
		return mutedAccent
	}
	if hex, ok := st.Accent[in.Severity]; ok {
		return parseHex(hex)
	}
	return builtinAccent[in.Severity]
}

func note(in *store.Incident) string {
	switch {
	case in.AckedAt != nil && in.AckedBy != nil:
		return "✓ Acknowledged by " + *in.AckedBy
	case in.SnoozedUntil != nil:
		return "Snoozed until " + in.SnoozedUntil.Format("15:04")
	}
	return ""
}

func titleCase(s string) string {
	if s == "" {
		return ""
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// parseHex reads a 6-digit hex colour; config validation guarantees it parses.
func parseHex(s string) int {
	n := 0
	for _, c := range s {
		n <<= 4
		switch {
		case c >= '0' && c <= '9':
			n |= int(c - '0')
		case c >= 'a' && c <= 'f':
			n |= int(c-'a') + 10
		case c >= 'A' && c <= 'F':
			n |= int(c-'A') + 10
		}
	}
	return n
}

// Short renders an elapsed duration for {duration}; the render layer calls it.
func Short(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return strconv.Itoa(int(d.Minutes())) + "m"
	default:
		return strconv.Itoa(int(d.Hours())) + "h"
	}
}
