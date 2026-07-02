package discord

import (
	"fmt"
	"sort"

	dgo "github.com/disgoorg/disgo/discord"
	"github.com/rotten-division/charon/internal/store"
)

const flagV2 = dgo.MessageFlagIsComponentsV2

func accent(sev string) int {
	switch sev {
	case "critical":
		return 0xE74C3C
	case "warning":
		return 0xE67E22
	default:
		return 0x3498DB
	}
}

func container(in *store.Incident) dgo.LayoutComponent {
	title := fmt.Sprintf("**%s**", in.Title)
	body := in.Body
	if in.Host != "" {
		body += "\nhost: " + in.Host
	}
	if len(in.Labels) > 0 {
		keys := make([]string, 0, len(in.Labels))
		for k := range in.Labels {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			body += fmt.Sprintf("\n%s: %s", k, in.Labels[k])
		}
	}
	if in.AckedBy != nil && in.AckedAt != nil {
		body += "\nacknowledged by " + *in.AckedBy
	}
	if in.SnoozedUntil != nil {
		body += "\nsnoozed until " + in.SnoozedUntil.Format("15:04")
	}
	col := accent(in.Severity)
	if in.AckedAt != nil {
		col = 0x95A5A6 // muted once acknowledged
	}
	return dgo.NewContainer(
		dgo.NewTextDisplay(title),
		dgo.NewTextDisplay(body),
		dgo.NewSmallSeparator(),
		dgo.NewActionRow(
			dgo.NewPrimaryButton("acknowledge", "/ack/"+in.DedupKey),
			dgo.NewSecondaryButton("snooze", "/snooze/"+in.DedupKey),
			dgo.NewDangerButton("resolve", "/resolve/"+in.DedupKey),
		),
	).WithAccentColor(col)
}

func RenderCreate(in *store.Incident) dgo.MessageCreate {
	return dgo.NewMessageCreateV2(container(in))
}

func RenderUpdate(in *store.Incident) dgo.MessageUpdate {
	return dgo.NewMessageUpdateV2(container(in))
}
