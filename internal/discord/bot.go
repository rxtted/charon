package discord

import (
	"context"
	"fmt"
	"time"

	"github.com/disgoorg/disgo"
	"github.com/disgoorg/disgo/bot"
	dgo "github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/handler"
	"github.com/disgoorg/snowflake/v2"
	"github.com/rotten-division/charon/internal/config"
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
		opts = append(opts, dgo.NewStringSelectMenuOption(d.String(), d.String()))
	}
	menu := dgo.NewStringSelectMenu("/snooze-pick/"+e.Vars["key"], "how long?", opts...)
	return e.CreateMessage(dgo.NewMessageCreate().
		WithFlags(dgo.MessageFlagEphemeral).
		WithContent("snooze this alert:").
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
	return dispatch(b.actions, "snooze", e.Vars["key"], e.User().Username, d)
}

func parseSnoozeValue(v string) (time.Duration, error) { return time.ParseDuration(v) }

// Sender wraps disgo REST into the queue's sender interface.
func (b *Bot) Sender() sender { return restSender{b.client} }

type restSender struct{ c *bot.Client }

func (r restSender) Post(channelID string, mc dgo.MessageCreate) (string, error) {
	m, err := r.c.Rest.CreateMessage(snowflake.MustParse(channelID), mc)
	if err != nil {
		return "", err
	}
	return m.ID.String(), nil
}

func (r restSender) Edit(channelID, msgID string, mu dgo.MessageUpdate) error {
	_, err := r.c.Rest.UpdateMessage(snowflake.MustParse(channelID), snowflake.MustParse(msgID), mu)
	return err
}

func (r restSender) DeleteMsg(channelID, msgID string) error {
	return r.c.Rest.DeleteMessage(snowflake.MustParse(channelID), snowflake.MustParse(msgID))
}

func (b *Bot) Open(ctx context.Context) error { return b.client.OpenGateway(ctx) }
func (b *Bot) Close()                         { b.client.Close(context.Background()) }
