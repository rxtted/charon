package discord

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/disgoorg/disgo"
	"github.com/disgoorg/disgo/bot"
	dgo "github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/handler"
	"github.com/disgoorg/disgo/rest"
	"github.com/disgoorg/snowflake/v2"
	"github.com/rxtted/charon/internal/config"
)

type Actions interface {
	Acknowledge(ctx context.Context, key, user string) error
	Snooze(ctx context.Context, key, user string, d time.Duration) error
	Resolve(ctx context.Context, key, user string) error
}

type Bot struct {
	client  *bot.Client
	cfg     config.Config
	actions Actions
}

// NewBot builds the gateway client and wires the interaction router. actions may be
// nil at construction (see SetActions): the core and the bot are constructed in
// either order, and dispatch is nil-safe until the core is wired in.
func NewBot(cfg config.Config, actions Actions) (*Bot, error) {
	b := &Bot{cfg: cfg, actions: actions}
	client, err := disgo.New(cfg.DiscordToken,
		bot.WithDefaultGateway(), // zero intents (IntentsDefault == IntentsNone); interactions don't need any
		bot.WithEventListeners(b.router()),
	)
	if err != nil {
		return nil, fmt.Errorf("build disgo client: %w", err)
	}
	b.client = client
	return b, nil
}

// SetActions late-binds the core once it's constructed. Call before Open.
func (b *Bot) SetActions(actions Actions) { b.actions = actions }

func (b *Bot) router() *handler.Mux {
	r := handler.New()
	r.ButtonComponent("/ack/{key}", b.button("ack"))
	r.ButtonComponent("/resolve/{key}", b.button("resolve"))
	r.ButtonComponent("/snooze/{key}", b.snoozePrompt)
	r.SelectMenuComponent("/snooze-pick/{key}", b.snoozePick)
	return r
}

// button defers immediately (protecting the 3s budget), then dispatches.
func (b *Bot) button(action string) handler.ButtonComponentHandler {
	return func(_ dgo.ButtonInteractionData, e *handler.ComponentEvent) error {
		if err := e.DeferUpdateMessage(); err != nil {
			return err
		}
		return dispatch(b.actions, action, e.Vars["key"], e.User().Username, 0)
	}
}

// dispatch maps an action verb onto the core. this is the pure, tested unit; the
// disgo handlers above are thin wrappers that defer and parse, then call it.
func dispatch(a Actions, action, key, user string, dur time.Duration) error {
	if a == nil {
		return nil // gateway opens only after SetActions, but guard anyway
	}
	switch action {
	case "ack":
		return a.Acknowledge(context.Background(), key, user)
	case "resolve":
		return a.Resolve(context.Background(), key, user)
	case "snooze":
		return a.Snooze(context.Background(), key, user, dur)
	default:
		return fmt.Errorf("unknown action %q", action)
	}
}

func (b *Bot) snoozePrompt(_ dgo.ButtonInteractionData, e *handler.ComponentEvent) error {
	opts := make([]dgo.StringSelectMenuOption, 0, len(b.cfg.SnoozeOptions))
	for _, d := range b.cfg.SnoozeOptions {
		opts = append(opts, dgo.NewStringSelectMenuOption(humanizeDur(d), d.String()))
	}
	menu := dgo.NewStringSelectMenu("/snooze-pick/"+e.Vars["key"], "How long?", opts...)
	return e.CreateMessage(dgo.NewMessageCreate().
		WithFlags(dgo.MessageFlagEphemeral).
		WithContent("Snooze this alert").
		AddActionRow(menu))
}

func (b *Bot) snoozePick(data dgo.SelectMenuInteractionData, e *handler.ComponentEvent) error {
	if err := e.DeferUpdateMessage(); err != nil {
		return err
	}
	sel, ok := data.(dgo.StringSelectMenuInteractionData)
	if !ok || len(sel.Values) == 0 {
		return fmt.Errorf("snooze-pick: unexpected component data %T", data)
	}
	d, err := parseSnoozeValue(sel.Values[0])
	if err != nil {
		return err
	}
	if err := dispatch(b.actions, "snooze", e.Vars["key"], e.User().Username, d); err != nil {
		return err
	}
	// collapse the ephemeral picker into a confirmation so the dropdown doesn't linger.
	_, err = e.UpdateInteractionResponse(dgo.NewMessageUpdate().
		WithContent("Snoozed for " + humanizeDur(d)).
		WithComponents())
	return err
}

func parseSnoozeValue(v string) (time.Duration, error) { return time.ParseDuration(v) }

// humanizeDur renders a snooze option as "15m" / "1h" / "1h 30m" rather than go's
// raw "15m0s".
func humanizeDur(d time.Duration) string {
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	switch {
	case h > 0 && m > 0:
		return fmt.Sprintf("%dh %dm", h, m)
	case h > 0:
		return fmt.Sprintf("%dh", h)
	default:
		return fmt.Sprintf("%dm", m)
	}
}

// Sender wraps disgo REST into the queue's sender interface.
func (b *Bot) Sender() sender { return restSender{b.client} }

type restSender struct{ c *bot.Client }

func (r restSender) Post(ctx context.Context, channelID string, mc dgo.MessageCreate) (string, error) {
	m, err := r.c.Rest.CreateMessage(snowflake.MustParse(channelID), mc, rest.WithCtx(ctx))
	if err != nil {
		return "", err
	}
	return m.ID.String(), nil
}

func (r restSender) Edit(ctx context.Context, channelID, msgID string, mu dgo.MessageUpdate) error {
	_, err := r.c.Rest.UpdateMessage(snowflake.MustParse(channelID), snowflake.MustParse(msgID), mu, rest.WithCtx(ctx))
	return err
}

func (r restSender) DeleteMsg(ctx context.Context, channelID, msgID string) error {
	return r.c.Rest.DeleteMessage(snowflake.MustParse(channelID), snowflake.MustParse(msgID), rest.WithCtx(ctx))
}

func (b *Bot) Open(ctx context.Context) error { return b.client.OpenGateway(ctx) }
func (b *Bot) Close()                         { b.client.Close(context.Background()) }

// SweepOrphans is a one-shot boot reconciliation: a crash between posting a
// card and writing its message_id can leave a bot-authored card with no matching
// incident. for each channel it lists the bot's own recent messages and deletes
// any whose id isn't in keep (the message ids of currently active incidents).
// best-effort: a channel listing failure or a per-message delete failure is
// logged and does not block startup or abort the remaining channels.
func (b *Bot) SweepOrphans(ctx context.Context, channelIDs []string, keep map[string]bool) error {
	self := b.client.ApplicationID // a bot's user id is its application id
	for _, chID := range channelIDs {
		cid, err := snowflake.Parse(chID)
		if err != nil {
			slog.Error("sweep orphans: bad channel id", "channel", chID, "err", err)
			continue
		}
		msgs, err := b.client.Rest.GetMessages(cid, 0, 0, 0, 100, rest.WithCtx(ctx))
		if err != nil {
			slog.Error("sweep orphans: list messages failed", "channel", chID, "err", err)
			continue
		}
		for _, id := range orphanIDs(msgs, self, keep) {
			if err := b.client.Rest.DeleteMessage(cid, id, rest.WithCtx(ctx)); err != nil {
				slog.Error("sweep orphans: delete failed", "channel", chID, "message", id.String(), "err", err)
			}
		}
	}
	return nil
}

// orphanIDs returns the ids of self-authored messages in msgs that aren't in
// keep. split out from SweepOrphans so the selection logic is testable without
// a live discord REST client.
func orphanIDs(msgs []dgo.Message, self snowflake.ID, keep map[string]bool) []snowflake.ID {
	var out []snowflake.ID
	for _, m := range msgs {
		if m.Author.ID != self || keep[m.ID.String()] {
			continue
		}
		out = append(out, m.ID)
	}
	return out
}
