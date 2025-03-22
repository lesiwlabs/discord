// labs.lesiw.io/discord is a Discord bot for lesiw.chat.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	_ "net/http/pprof"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/disgoorg/disgo"
	disgobot "github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/cache"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/disgo/gateway"
	"github.com/disgoorg/snowflake/v2"
	_ "golang.org/x/crypto/x509roots/fallback"
)

var t testingDetector

//go:generate go run lesiw.io/moxie@latest client
type client struct{ disgobot.Client }

func main() {
	go func() {
		slog.Error(http.ListenAndServe("localhost:8080", nil).Error())
	}()
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
	bot = &client{bot}
	bot.AddEventListeners(
		disgobot.NewListenerFunc(func(*events.Ready) {
			slog.Info("received ready event from gateway")
		}),
		disgobot.NewListenerFunc(func(e *events.GuildReady) {
			go func() {
				ticker := time.NewTicker(time.Minute)
				for range ticker.C {
					if err := syncVoiceRoles(bot, e.GuildID); err != nil {
						slog.Error("failed to sync voice roles", "error", err)
					}
				}
			}()
		}),
		disgobot.NewListenerFunc(func(e *events.GuildVoiceJoin) {
			if err := toggleVoiceRole(e); err != nil {
				slog.Error(err.Error())
			}
		}),
		disgobot.NewListenerFunc(func(e *events.GuildVoiceLeave) {
			if err := toggleVoiceRole(e); err != nil {
				slog.Error(err.Error())
			}
		}))
	if err != nil {
		return fmt.Errorf("could not set up bot: %w", err)
	}
	if err := bot.OpenGateway(context.Background()); err != nil {
		return fmt.Errorf("could not connect to gateway: %w", err)
	}
	select {}
}

var testHookFindRoleByName func(
	disgobot.Client, snowflake.ID, string,
) (discord.Role, error)

func findRoleByName(
	c disgobot.Client, gid snowflake.ID, name string,
) (discord.Role, error) {
	if h := testHookFindRoleByName; t.Testing() && h != nil {
		return h(c, gid, name)
	}
	roles, err := c.Rest().GetRoles(gid)
	if err != nil {
		return discord.Role{}, fmt.Errorf("could not get roles: %w", err)
	}
	for _, r := range roles {
		if r.Name == name {
			return r, nil
		}
	}
	return discord.Role{}, fmt.Errorf("could not find role %q", name)
}

var testHookToggleRole func(
	disgobot.Client, bool, snowflake.ID, snowflake.ID, snowflake.ID,
) error

func toggleRole(
	bot disgobot.Client, state bool,
	guildID, userID, roleID snowflake.ID,
) error {
	if h := testHookToggleRole; t.Testing() && h != nil {
		return h(bot, state, guildID, userID, roleID)
	}
	fn := bot.Rest().RemoveMemberRole
	if state {
		fn = bot.Rest().AddMemberRole
	}
	var roleName, userName string
	if role, ok := bot.Caches().Role(guildID, roleID); ok {
		roleName = role.Name
	} else {
		roleName = "<unknown>"
	}
	if user, ok := bot.Caches().Member(guildID, userID); ok {
		userName = user.User.Username
	} else {
		userName = "<unknown>"
	}
	if err := fn(guildID, userID, roleID); err != nil {
		return fmt.Errorf("failed to toggle role %q (enable=%t): %w",
			roleName, state, err)
	}
	slog.Info("role toggle",
		"role", roleName,
		"user", userName,
		"enable", state,
	)
	return nil
}

var voiceToggle sync.Mutex

type voiceEvent interface {
	*events.GuildVoiceJoin | *events.GuildVoiceLeave
}

func toggleVoiceRole[E voiceEvent](e E) error {
	voiceToggle.Lock()
	defer voiceToggle.Unlock()
	var enable bool
	var event events.GenericGuildVoiceState
	switch e2 := any(e).(type) {
	case *events.GuildVoiceJoin:
		enable = true
		event = *e2.GenericGuildVoiceState
	case *events.GuildVoiceLeave:
		event = *e2.GenericGuildVoiceState
	default:
		panic("unreachable")
	}
	client := event.Client()
	role, err := findRoleByName(client, event.VoiceState.GuildID, "voice")
	if err != nil {
		return err
	}
	return toggleRole(client, enable,
		event.VoiceState.GuildID, event.Member.User.ID, role.ID)
}

func syncVoiceRoles(bot disgobot.Client, gid snowflake.ID) error {
	slog.Info("syncVoiceRoles tick")
	voiceToggle.Lock()
	defer voiceToggle.Unlock()
	role, err := findRoleByName(bot, gid, "voice")
	if err != nil {
		return fmt.Errorf("could not get voice role: %w", err)
	}
	roleMembers, err := membersWithRole(bot, gid, role)
	if err != nil {
		return err
	}
	slog.Info("got role members", "members", memberList(bot, gid, roleMembers))
	callMembers := membersInCall(bot, gid)
	slog.Info("got call members", "members", memberList(bot, gid, callMembers))
	for uid := range roleMembers.Diff(callMembers) {
		// Members that are not in the call, but have a role.
		if err := toggleRole(bot, false, gid, uid, role.ID); err != nil {
			return fmt.Errorf("could not remove role: %w", err)
		}
	}
	for uid := range callMembers.Diff(roleMembers) {
		// Members that are in the call, but have no role.
		if err := toggleRole(bot, true, gid, uid, role.ID); err != nil {
			return fmt.Errorf("could not add role: %w", err)
		}
	}
	return nil
}

var testHookMemberList func(
	disgobot.Client, snowflake.ID, set[snowflake.ID],
) string

func memberList(
	bot disgobot.Client, guildID snowflake.ID, m set[snowflake.ID],
) string {
	if h := testHookMemberList; t.Testing() && h != nil {
		return h(bot, guildID, m)
	}
	var names []string
	for id := range m {
		if user, ok := bot.Caches().Member(guildID, id); ok {
			names = append(names, user.User.Username)
		} else {
			names = append(names, "<unknown>")
		}
	}
	if len(names) == 0 {
		return "<none>"
	}
	return strings.Join(names, ", ")
}

var testHookMembersWithRole func(
	disgobot.Client, snowflake.ID, discord.Role,
) (set[snowflake.ID], error)

func membersWithRole(
	bot disgobot.Client, gid snowflake.ID, role discord.Role,
) (set[snowflake.ID], error) {
	if h := testHookMembersWithRole; t.Testing() && h != nil {
		return h(bot, gid, role)
	}
	members, err := bot.MemberChunkingManager().RequestMembersWithFilter(
		gid,
		func(m discord.Member) bool {
			return slices.Contains(m.RoleIDs, role.ID)
		},
	)
	if err != nil {
		return nil, fmt.Errorf("could not get members with role %q: %w",
			role.Name, err)
	}
	s := newSet[snowflake.ID]()
	for _, m := range members {
		s.Add(m.User.ID)
	}
	return s, nil
}

var testHookMembersInCall func(disgobot.Client, snowflake.ID) set[snowflake.ID]

func membersInCall(bot disgobot.Client, gid snowflake.ID) set[snowflake.ID] {
	if h := testHookMembersInCall; t.Testing() && h != nil {
		return h(bot, gid)
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
	s := newSet[snowflake.ID]()
	for _, m := range voiceMembers {
		s.Add(m.User.ID)
	}
	return s
}
