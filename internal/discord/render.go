package discord

import (
	"strings"
	"time"

	dgo "github.com/disgoorg/disgo/discord"
	"github.com/rxtted/charon/internal/card"
)

const flagV2 = dgo.MessageFlagIsComponentsV2

// noMentions is applied to every rendered message: incident cards are built from
// ingest text an attacker controls, so discord must never parse an @everyone/@here/
// role/user mention out of it.
var noMentions = &dgo.AllowedMentions{}

func RenderCreate(r card.Rendered, wrap int) dgo.MessageCreate {
	return dgo.NewMessageCreateV2(container(r, wrap)).WithAllowedMentions(noMentions)
}

func RenderUpdate(r card.Rendered, wrap int) dgo.MessageUpdate {
	return dgo.NewMessageUpdateV2(container(r, wrap)).WithAllowedMentions(noMentions)
}

func container(r card.Rendered, wrap int) dgo.LayoutComponent {
	hasFill := r.Description != "" || len(r.Data) > 0 || len(r.Glance) > 0
	useIcon := r.Icon != "" && hasFill

	w := wrap // a non-zero per-sender wrap wins over the global default
	if r.Wrap > 0 {
		w = r.Wrap
	}

	var inner []dgo.ContainerSubComponent
	gap := dgo.NewSeparator(dgo.SeparatorSpacingSizeSmall).WithDivider(false)

	glance := glanceLine(r)
	data := dataLines(r.Data)

	if useIcon {
		subs := []dgo.SectionSubComponent{dgo.NewTextDisplay("### " + r.Title)}
		if r.Description != "" {
			subs = append(subs, dgo.NewTextDisplay("\n"+wrapText(r.Description, w)))
		}
		// with no description the section is just the title, shorter than the
		// thumbnail's fixed height; pack the glance and first data line in to fill it.
		belowGlance, belowData := glance, data
		if r.Description == "" {
			if belowGlance != "" && len(subs) < 3 {
				subs = append(subs, dgo.NewTextDisplay(belowGlance))
				belowGlance = ""
			}
			if len(belowData) > 0 && len(subs) < 3 {
				subs = append(subs, dgo.NewTextDisplay(belowData[0]))
				belowData = belowData[1:]
			}
		}
		inner = append(inner, dgo.NewSection(subs...).WithAccessory(dgo.NewThumbnail(r.Icon)), gap)
		if belowGlance != "" {
			inner = append(inner, dgo.NewTextDisplay(belowGlance))
		}
		for _, d := range belowData {
			inner = append(inner, dgo.NewTextDisplay(d))
		}
	} else {
		inner = append(inner, dgo.NewTextDisplay("### "+r.Title))
		if r.Description != "" {
			inner = append(inner, dgo.NewTextDisplay("\n"+wrapText(r.Description, w)))
		}
		inner = append(inner, gap)
		if glance != "" {
			inner = append(inner, dgo.NewTextDisplay(glance))
		}
		for _, d := range data {
			inner = append(inner, dgo.NewTextDisplay(d))
		}
	}

	if len(r.Footer) > 0 {
		parts := make([]string, len(r.Footer))
		for i, f := range r.Footer {
			parts[i] = expandDuration(f, r.CreatedAt)
		}
		inner = append(inner, dgo.NewTextDisplay("-# "+strings.Join(parts, " · ")))
	}
	inner = append(inner, dgo.NewSmallSeparator())
	if r.Note != "" {
		inner = append(inner, dgo.NewTextDisplay("-# "+r.Note))
	}
	inner = append(inner, actionRow(r))
	return dgo.NewContainer(inner...).WithAccentColor(r.Accent)
}

func glanceLine(r card.Rendered) string {
	var parts []string
	if r.Severity != "" {
		parts = append(parts, "**"+r.Severity+"**")
	}
	for _, g := range r.Glance {
		v := g.Value
		if g.Code {
			v = "`" + v + "`"
		}
		parts = append(parts, v)
	}
	return strings.Join(parts, " · ")
}

func dataLines(items []card.DataItem) []string {
	out := make([]string, 0, len(items))
	for _, d := range items {
		if d.Label != "" {
			out = append(out, d.Label+": "+d.Value)
		} else {
			out = append(out, d.Value)
		}
	}
	return out
}

// the three lifecycle buttons stay one style so the row renders at a single
// height (discord sizes buttons by style); link buttons match.
func actionRow(r card.Rendered) dgo.ActionRowComponent {
	row := []dgo.InteractiveComponent{
		dgo.NewSecondaryButton("Acknowledge", "/ack/"+r.DedupKey),
		dgo.NewSecondaryButton("Snooze", "/snooze/"+r.DedupKey),
		dgo.NewSecondaryButton("Resolve", "/resolve/"+r.DedupKey),
	}
	for _, l := range r.Links {
		row = append(row, dgo.NewLinkButton(l.Label, l.URL))
	}
	return dgo.NewActionRow(row...)
}

func expandDuration(s string, created time.Time) string {
	if !strings.Contains(s, "{duration}") {
		return s
	}
	return strings.ReplaceAll(s, "{duration}", card.Short(time.Since(created)))
}

// discord has no width control on a text display: a long paragraph fills the
// message to its max width. wrapText hard-wraps the body to a column to bound it.
func wrapText(s string, width int) string {
	var lines []string
	var line string
	for _, word := range strings.Fields(s) {
		switch {
		case line == "":
			line = word
		case len(line)+1+len(word) <= width:
			line += " " + word
		default:
			lines = append(lines, line)
			line = word
		}
	}
	if line != "" {
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}
