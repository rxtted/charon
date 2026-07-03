package discord

import (
	"strings"
	"testing"

	dgo "github.com/disgoorg/disgo/discord"
	"github.com/rxtted/charon/internal/card"
	"github.com/rxtted/charon/internal/event"
)

// walk yields every component in mc, recursing into containers/sections/action rows
// via the ComponentIter interface disgo's builders implement.
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
		if b, ok := c.(dgo.ButtonComponent); ok && b.CustomID != "" {
			ids = append(ids, b.CustomID)
		}
	}
	return ids
}

// thumbnailURLs pulls the section-accessory thumbnails, so a test can tell whether
// the hybrid icon rule actually attached one (renderText can't, it's not text).
func thumbnailURLs(mc dgo.MessageCreate) []string {
	var urls []string
	for _, c := range walk(mc.Components) {
		if th, ok := c.(dgo.ThumbnailComponent); ok {
			urls = append(urls, th.Media.URL)
		}
	}
	return urls
}

func linkButtonURLs(mc dgo.MessageCreate) []string {
	var urls []string
	for _, c := range walk(mc.Components) {
		if b, ok := c.(dgo.ButtonComponent); ok && b.URL != "" {
			urls = append(urls, b.URL)
		}
	}
	return urls
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

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s || strings.HasPrefix(x, s) {
			return true
		}
	}
	return false
}

func TestRenderV2AndCustomIDs(t *testing.T) {
	r := card.Rendered{Title: "host down", Severity: "Critical", Description: "d", DedupKey: "k1"}
	mc := RenderCreate(r, 52)
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

// incident cards render attacker-controlled ingest text, so both create and update
// carry a non-nil, empty AllowedMentions that parses nothing out of that text.
func TestRenderNoMentions(t *testing.T) {
	r := card.Rendered{Title: "@everyone t", Severity: "Critical"}
	for _, m := range []*dgo.AllowedMentions{RenderCreate(r, 52).AllowedMentions, RenderUpdate(r, 52).AllowedMentions} {
		if m == nil {
			t.Fatal("AllowedMentions must not be nil")
		}
		if len(m.Parse) != 0 || len(m.Roles) != 0 || len(m.Users) != 0 || m.RepliedUser {
			t.Fatalf("AllowedMentions must parse nothing, got %+v", m)
		}
	}
}

func TestRenderShowsAck(t *testing.T) {
	r := card.Rendered{Title: "t", Severity: "Critical", Description: "d", Note: "✓ Acknowledged by noah"}
	if !strings.Contains(renderText(RenderCreate(r, 52)), "Acknowledged by noah") {
		t.Fatal("ack not rendered")
	}
}

func TestRenderBareCardDropsIcon(t *testing.T) {
	r := card.Rendered{Title: "Nightly backup completed", Severity: "Info", Icon: "https://x/i.png",
		Footer: []string{"restic"}}
	mc := RenderCreate(r, 52)
	if len(thumbnailURLs(mc)) != 0 {
		t.Fatalf("bare card (no description/glance/data) must drop the thumbnail, got %v", thumbnailURLs(mc))
	}
	if !strings.Contains(renderText(mc), "### Nightly backup completed") {
		t.Fatal("title should render as a heading")
	}
}

func TestRenderKeepsIconWhenFilled(t *testing.T) {
	r := card.Rendered{Title: "GPU hot", Severity: "Warning", Icon: "https://x/i.png",
		Glance: []card.GlanceItem{{Value: "temp = 88", Code: true}}}
	mc := RenderCreate(r, 52)
	if !contains(thumbnailURLs(mc), "https://x/i.png") {
		t.Fatalf("a filled card must render the thumbnail, got %v", thumbnailURLs(mc))
	}
	if !strings.Contains(renderText(mc), "`temp = 88`") {
		t.Fatal("code glance item must render in inline code")
	}
}

func TestRenderButtonsAndLinks(t *testing.T) {
	r := card.Rendered{Title: "t", Severity: "Critical", Description: "d", DedupKey: "k9",
		Links: []card.Link{{Label: "Open", URL: "https://g/1"}}}
	mc := RenderCreate(r, 52)
	ids := customIDs(mc)
	for _, want := range []string{"/ack/k9", "/snooze/k9", "/resolve/k9"} {
		if !contains(ids, want) {
			t.Fatalf("missing lifecycle button %q in %v", want, ids)
		}
	}
	if !contains(linkButtonURLs(mc), "https://g/1") {
		t.Fatalf("configured link button missing, got %v", linkButtonURLs(mc))
	}
}

func TestRenderFooterIsOneLine(t *testing.T) {
	r := card.Rendered{Title: "t", Severity: "Info", Description: "d",
		Footer: []string{"titan", "grafana", "20:14"}}
	txt := renderText(RenderCreate(r, 52))
	if strings.Count(txt, "-# ") != 1 {
		t.Fatalf("footer should be one muted line, got %d", strings.Count(txt, "-# "))
	}
	if !strings.Contains(txt, "titan · grafana · 20:14") {
		t.Fatal("footer entries should join with ' · '")
	}
}

func TestRenderNotifyShowsOnlyAcknowledge(t *testing.T) {
	r := card.Rendered{Title: "backup done", Severity: "Info", Kind: event.Notify, DedupKey: "n1"}
	ids := customIDs(RenderCreate(r, 52))
	if !contains(ids, "/ack/n1") {
		t.Fatalf("notify should have Acknowledge, got %v", ids)
	}
	for _, unwanted := range []string{"/snooze/", "/resolve/"} {
		if contains(ids, unwanted) {
			t.Fatalf("notify must not show %s, got %v", unwanted, ids)
		}
	}
}

func TestRenderAlertShowsAllThree(t *testing.T) {
	r := card.Rendered{Title: "host down", Severity: "Critical", Kind: event.Alert, DedupKey: "a1"}
	ids := customIDs(RenderCreate(r, 52))
	for _, want := range []string{"/ack/a1", "/snooze/a1", "/resolve/a1"} {
		if !contains(ids, want) {
			t.Fatalf("alert missing %s, got %v", want, ids)
		}
	}
}
