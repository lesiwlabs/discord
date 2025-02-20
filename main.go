// labs.lesiw.io/discord is a Discord bot for lesiw.chat.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/disgoorg/disgo"
	disgobot "github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/disgo/gateway"
	"github.com/disgoorg/disgo/rest"
	_ "golang.org/x/crypto/x509roots/fallback"
)

func main() {
	if err := run(); err != nil {
		slog.Error(err.Error())
		os.Exit(1)
	}
}

func run() error {
	tok := os.Getenv("DISCORD_TOKEN")
	if tok == "" {
		return fmt.Errorf("bad DISCORD_TOKEN")
	}
	bot, err := disgo.New(tok,
		disgobot.WithGatewayConfigOpts(
			gateway.WithIntents(
				gateway.IntentGuilds,
				gateway.IntentGuildMessages,
				gateway.IntentDirectMessages,
				gateway.IntentGuildVoiceStates,
				gateway.IntentGuildMessageReactions,
			),
		),
		disgobot.WithEventListenerFunc(func(e *events.GuildVoiceJoin) {
			err := toggleVoiceRole(
				e.Client().Rest(),
				true,
				e.GenericGuildVoiceState,
			)
			if err != nil {
				slog.Error("failed to add member role", "error", err)
			}
		}),
		disgobot.WithEventListenerFunc(func(e *events.GuildVoiceLeave) {
			err := toggleVoiceRole(
				e.Client().Rest(),
				false,
				e.GenericGuildVoiceState,
			)
			if err != nil {
				slog.Error("failed to remove member role", "error", err)
			}
		}),
	)
	if err != nil {
		return fmt.Errorf("could not set up bot: %w", err)
	}
	if err := bot.OpenGateway(context.Background()); err != nil {
		return fmt.Errorf("could not connect to gateway: %w", err)
	}
	select {}
}

func toggleVoiceRole(
	bot rest.Rest, apply bool, e *events.GenericGuildVoiceState,
) error {
	roles, err := bot.GetRoles(e.VoiceState.GuildID)
	if err != nil {
		return fmt.Errorf("could not get roles: %w", err)
	}
	var role discord.Role
	rolename := "voice"
	for _, r := range roles {
		if r.Name == rolename {
			role = r
			goto found
		}
	}
	return fmt.Errorf("could not find the role %q", rolename)
found:

	fn := bot.RemoveMemberRole
	if apply {
		fn = bot.AddMemberRole
	}
	if err := fn(e.VoiceState.GuildID, e.Member.User.ID, role.ID); err != nil {
		return fmt.Errorf("failed to toggle role (apply = %t): %w", apply, err)
	}
	return nil
}
