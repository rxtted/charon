package discord

import (
	"strings"
	"testing"
	"time"

	dgo "github.com/disgoorg/disgo/discord"
	"github.com/rxttd/cheron/internal/store"
)

// walk yields every component in mc, recursing into containers/action rows via
// the ComponentIter interface disgo's builders implement.
func walk(components []dgo.LayoutComponent) []dgo.Component {
	var out []dgo.Component
	for _, c := range components {
		out = append(out, c)
		if it, ok := c.(dgo.ComponentIter); ok {
			for sub := range it.SubComponents() {
				out = append(out, sub)
			}
		}
	}
	return out
}

func customIDs(mc dgo.MessageCreate) []string {
	var ids []string
	for _, c := range walk(mc.Components) {
		if b, ok := c.(dgo.ButtonComponent); ok {
			ids = append(ids, b.CustomID)
		}
	}
	return ids
}

func renderText(mc dgo.MessageCreate) string {
	var parts []string
	for _, c := range walk(mc.Components) {
		if td, ok := c.(dgo.TextDisplayComponent); ok {
			parts = append(parts, td.Content)
		}
	}
	return strings.Join(parts, "\n")
}

func TestRenderCreateHasV2FlagAndButtons(t *testing.T) {
	in := &store.Incident{DedupKey: "k1", Severity: "critical", Title: "host down", Host: "host-a"}
	mc := RenderCreate(in)
	if mc.Flags&flagV2 == 0 {
		t.Fatal("components v2 flag not set")
	}
	ids := customIDs(mc) // custom ids must round-trip the dedup key for the router
	for _, want := range []string{"/ack/k1", "/snooze/k1", "/resolve/k1"} {
		if !contains(ids, want) {
			t.Fatalf("missing custom id %q in %v", want, ids)
		}
	}
}

func TestAccentBySeverity(t *testing.T) {
	if accent("critical") == accent("warning") || accent("warning") == accent("info") {
		t.Fatal("severities must map to distinct accent colours")
	}
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s || strings.HasPrefix(x, s) {
			return true
		}
	}
	return false
}

// TestRenderAllowedMentionsIsEmpty: incident cards render attacker-
// controlled ingest text, so both create and update messages must carry a
// non-nil, empty AllowedMentions that parses nothing out of that text.
func TestRenderAllowedMentionsIsEmpty(t *testing.T) {
	in := &store.Incident{DedupKey: "k", Severity: "critical", Title: "@everyone t"}
	create := RenderCreate(in)
	if create.AllowedMentions == nil {
		t.Fatal("create: AllowedMentions must not be nil")
	}
	if len(create.AllowedMentions.Parse) != 0 || len(create.AllowedMentions.Roles) != 0 ||
		len(create.AllowedMentions.Users) != 0 || create.AllowedMentions.RepliedUser {
		t.Fatalf("create: AllowedMentions must parse nothing, got %+v", create.AllowedMentions)
	}

	update := RenderUpdate(in)
	if update.AllowedMentions == nil {
		t.Fatal("update: AllowedMentions must not be nil")
	}
	if len(update.AllowedMentions.Parse) != 0 || len(update.AllowedMentions.Roles) != 0 ||
		len(update.AllowedMentions.Users) != 0 || update.AllowedMentions.RepliedUser {
		t.Fatalf("update: AllowedMentions must parse nothing, got %+v", update.AllowedMentions)
	}
}

// the mirror of the incident package's hash-coverage test: an acked incident must
// render its ack line and a muted accent, proving those fields reach the render.
func TestRenderShowsAck(t *testing.T) {
	now := time.Now()
	who := "noah"
	in := &store.Incident{DedupKey: "k", Severity: "critical", Title: "t", AckedAt: &now, AckedBy: &who}
	if !strings.Contains(renderText(RenderCreate(in)), "acknowledged by noah") {
		t.Fatal("ack not rendered")
	}
}
