// labs.lesiw.io/discord is a Discord bot for lesiw.chat.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"time"

	"github.com/disgoorg/disgo"
	disgobot "github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/cache"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/disgo/gateway"
	"github.com/disgoorg/disgo/rest"
	"github.com/disgoorg/snowflake/v2"
	_ "golang.org/x/crypto/x509roots/fallback"
)

var t testingDetector

const voiceRoleName = "voice"

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
				gateway.IntentGuildMembers,
				gateway.IntentGuildMessages,
				gateway.IntentGuildVoiceStates,
				gateway.IntentGuildMessageReactions,
			),
		),
		disgobot.WithCacheConfigOpts(
			cache.WithCaches(cache.FlagGuilds|
				cache.FlagChannels|
				cache.FlagMembers|
				cache.FlagVoiceStates|
				cache.FlagRoles,
			),
		),
	)
	bot.AddEventListeners(
		disgobot.NewListenerFunc(func(*events.Ready) {
			slog.Info("received ready event from gateway")
		}),
		disgobot.NewListenerFunc(func(e *events.GuildReady) {
			go func() {
				ticker := time.NewTicker(time.Minute)
				for ; ; <-ticker.C {
					if err := syncVoiceRoles(bot, e.GuildID); err != nil {
						slog.Error("failed to sync voice roles", "error", err)
					}
				}
			}()
		}),
		disgobot.NewListenerFunc(func(e *events.GuildVoiceJoin) {
			err := toggleVoiceRole(
				e.Client().Rest(),
				true,
				e.GenericGuildVoiceState,
			)
			if err != nil {
				slog.Error("failed to add member role", "error", err)
			}
		}),
		disgobot.NewListenerFunc(func(e *events.GuildVoiceLeave) {
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

var testHookVoiceRole func(rest.Rest, snowflake.ID) (discord.Role, error)

func voiceRole(bot rest.Rest, gid snowflake.ID) (discord.Role, error) {
	if h := testHookVoiceRole; t.Testing() && h != nil {
		return h(bot, gid)
	}
	roles, err := bot.GetRoles(gid)
	if err != nil {
		return discord.Role{}, fmt.Errorf("could not get roles: %w", err)
	}
	for _, r := range roles {
		if r.Name == voiceRoleName {
			return r, nil
		}
	}
	return discord.Role{}, fmt.Errorf("could not find the role %q",
		voiceRoleName)
}

func toggleVoiceRole(
	bot rest.Rest, apply bool, e *events.GenericGuildVoiceState,
) error {
	role, err := voiceRole(bot, e.VoiceState.GuildID)
	if err != nil {
		return err
	}
	fn := bot.RemoveMemberRole
	if apply {
		fn = bot.AddMemberRole
	}
	if err := fn(e.VoiceState.GuildID, e.Member.User.ID, role.ID); err != nil {
		return fmt.Errorf("failed to toggle role (apply = %t): %w", apply, err)
	}
	return nil
}

func syncVoiceRoles(bot disgobot.Client, gid snowflake.ID) error {
	role, err := voiceRole(bot.Rest(), gid)
	if err != nil {
		return fmt.Errorf("could not get voice role: %w", err)
	}
	roleMembers, err := bot.MemberChunkingManager().RequestMembersWithFilter(
		gid,
		func(m discord.Member) bool {
			return slices.Contains(m.RoleIDs, role.ID)
		},
	)
	if err != nil {
		return fmt.Errorf("could not get members with voice role: %w", err)
	}
	var voiceMembers []discord.Member
	bot.Caches().ChannelsForEach(func(channel discord.GuildChannel) {
		if channel.GuildID() != gid {
			return
		}
		ac, ok := channel.(discord.GuildAudioChannel)
		if !ok {
			return
		}
		voiceMembers = append(voiceMembers,
			bot.Caches().AudioChannelMembers(ac)...)
	})
	var hasRole, inCall = newSet[snowflake.ID](), newSet[snowflake.ID]()
	usermap := make(map[snowflake.ID]discord.Member)
	for _, m := range roleMembers {
		hasRole.Add(m.User.ID)
		usermap[m.User.ID] = m
	}
	for _, m := range voiceMembers {
		inCall.Add(m.User.ID)
		usermap[m.User.ID] = m
	}
	for uid := range hasRole.Diff(inCall) {
		// Members that are not in the call, but have a role.
		if err := bot.Rest().RemoveMemberRole(gid, uid, role.ID); err != nil {
			return fmt.Errorf("could not remove role: %w", err)
		}
		slog.Info("removed voice role", "user", usermap[uid].User.Username)
	}
	for uid := range inCall.Diff(hasRole) {
		// Members that are in the call, but have no role.
		if err := bot.Rest().AddMemberRole(gid, uid, role.ID); err != nil {
			return fmt.Errorf("could not add role: %w", err)
		}
		slog.Info("applied voice role", "user", usermap[uid].User.Username)
	}
	return nil
}
