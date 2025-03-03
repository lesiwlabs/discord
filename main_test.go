package main

import (
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"strings"
	"testing"

	disgobot "github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/disgo/rest"
	"github.com/disgoorg/snowflake/v2"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

//go:generate go run lesiw.io/moxie@latest clientRest
type clientRest struct{ rest.Rest }

func mockClient(_ *testing.T) *client {
	c := new(client)
	c._Rest_Return(new(clientRest))
	return c
}

type findRolesTest struct {
	desc        string
	roleName    string
	roles       []discord.Role
	getRolesErr error
	guildID     snowflake.ID
	wantErr     error
	wantRole    discord.Role
}

var findRolesTests = []findRolesTest{{
	desc:        "GetRoles() error",
	roleName:    "voice",
	roles:       nil,
	getRolesErr: errors.New("boom"),
	guildID:     42,
	wantErr:     errors.New("could not get roles: boom"),
	wantRole:    discord.Role{},
}, {
	desc:        "no roles",
	roleName:    "voice",
	roles:       []discord.Role{},
	getRolesErr: nil,
	guildID:     42,
	wantErr:     errors.New(`could not find role "voice"`),
	wantRole:    discord.Role{},
}, {
	desc:        "role not found",
	roleName:    "voice",
	roles:       []discord.Role{{Name: "notvoice"}, {}},
	getRolesErr: nil,
	guildID:     42,
	wantErr:     errors.New(`could not find role "voice"`),
	wantRole:    discord.Role{},
}, {
	desc:        "role found",
	roleName:    "voice",
	roles:       []discord.Role{{Name: "notvoice"}, {Name: "voice"}},
	getRolesErr: nil,
	guildID:     42,
	wantErr:     nil,
	wantRole:    discord.Role{Name: "voice"},
}}

func TestFindRoleByName(t *testing.T) {
	for _, tt := range findRolesTests {
		t.Run(tt.desc, func(t *testing.T) {
			c := mockClient(t)
			c.Rest().(*clientRest)._GetRoles_Return(tt.roles, tt.getRolesErr)

			role, err := findRoleByName(c, tt.guildID, tt.roleName)

			gotErr, wantErr := fmt.Sprintf("%v", err),
				fmt.Sprintf("%v", tt.wantErr)
			if gotErr != wantErr {
				t.Errorf("%s(%T, %v, %v): %v, want %v",
					funcname(t, findRoleByName), c, tt.guildID, tt.roleName,
					err, tt.wantErr)
				return
			}
			if got, want := role, tt.wantRole; !cmp.Equal(got, want) {
				t.Errorf("%s(%T, %v, %v) -want +got\n%s",
					funcname(t, findRoleByName), c, tt.guildID, tt.roleName,
					cmp.Diff(want, got),
				)
			}
		})
	}
}

type toggleVoiceRoleTest struct {
	desc        string
	join        bool
	findRoleErr error
	toggleErr   error
	wantErr     error
}

var toggleVoiceRoleTests = []toggleVoiceRoleTest{{
	desc: "voice join event",
	join: true,
}, {
	desc: "voice leave event",
	join: false,
}, {
	desc:        "find role error",
	join:        true,
	findRoleErr: errors.New("find role error"),
	wantErr:     errors.New("find role error"),
}, {
	desc:      "toggle role error",
	join:      false,
	toggleErr: errors.New("toggle role error"),
	wantErr:   errors.New("toggle role error"),
}}

func TestToggleVoiceRole(t *testing.T) {
	for _, tt := range toggleVoiceRoleTests {
		t.Run(tt.desc, func(t *testing.T) {
			swap(t, &testHookFindRoleByName,
				func(
					disgobot.Client, snowflake.ID, string,
				) (discord.Role, error) {
					return discord.Role{}, tt.findRoleErr
				},
			)
			swap(t, &testHookToggleRole,
				func(
					_ disgobot.Client, enable bool, _, _, _ snowflake.ID,
				) error {
					if enable != tt.join {
						t.Errorf("%s called with state %t, want %t",
							funcname(t, toggleRole), enable, tt.join)
					}
					return tt.toggleErr
				},
			)

			var err error
			genericVoiceState := events.GenericGuildVoiceState{
				GenericEvent: &events.GenericEvent{},
			}
			if tt.join {
				err = toggleVoiceRole(&events.GuildVoiceJoin{
					GenericGuildVoiceState: &genericVoiceState,
				})
			} else {
				err = toggleVoiceRole(&events.GuildVoiceLeave{
					GenericGuildVoiceState: &genericVoiceState,
				})
			}

			gotErr, wantErr := fmt.Sprintf("%v", err),
				fmt.Sprintf("%v", tt.wantErr)
			if gotErr != wantErr {
				t.Errorf("%s(): %v, want %v",
					funcname(t, toggleVoiceRole[*events.GuildVoiceJoin]),
					gotErr, wantErr)
			}
		})
	}
}

type syncVoiceRolesTest struct {
	desc        string
	findRoleErr error
	roleMembers set[snowflake.ID]
	callMembers set[snowflake.ID]
	toggleOn    set[snowflake.ID]
	toggleOff   set[snowflake.ID]
	toggleErr   error
	wantErr     error
}

var syncVoiceRolesTests = []syncVoiceRolesTest{{
	desc: "no members",
}, {
	desc:        "all members have roles",
	roleMembers: newSet[snowflake.ID](1, 2, 3),
	callMembers: newSet[snowflake.ID](1, 2, 3),
	toggleOn:    newSet[snowflake.ID](),
	toggleOff:   newSet[snowflake.ID](),
}, {
	desc:        "one member missing role",
	roleMembers: newSet[snowflake.ID](1, 3),
	callMembers: newSet[snowflake.ID](1, 2, 3),
	toggleOn:    newSet[snowflake.ID](2),
	toggleOff:   newSet[snowflake.ID](),
}, {
	desc:        "one role too many",
	roleMembers: newSet[snowflake.ID](1, 2, 3),
	callMembers: newSet[snowflake.ID](1, 2),
	toggleOn:    newSet[snowflake.ID](),
	toggleOff:   newSet[snowflake.ID](3),
}, {
	desc:        "mixed state",
	roleMembers: newSet[snowflake.ID](1, 3),
	callMembers: newSet[snowflake.ID](1, 2, 4),
	toggleOn:    newSet[snowflake.ID](2, 4),
	toggleOff:   newSet[snowflake.ID](3),
}}

func TestSyncVoiceRoles(t *testing.T) {
	opts := []cmp.Option{
		cmpopts.SortMaps(func(x, y snowflake.ID) bool { return x < y }),
		cmpopts.EquateEmpty(),
	}
	for _, tt := range syncVoiceRolesTests {
		t.Run(tt.desc, func(t *testing.T) {
			toggleOn := newSet[snowflake.ID]()
			toggleOff := newSet[snowflake.ID]()
			swap(t, &testHookFindRoleByName,
				func(
					disgobot.Client, snowflake.ID, string,
				) (discord.Role, error) {
					return discord.Role{}, tt.findRoleErr
				},
			)
			swap(t, &testHookToggleRole,
				func(
					_ disgobot.Client, enable bool, _, uid, _ snowflake.ID,
				) error {
					if enable {
						toggleOn.Add(uid)
					} else {
						toggleOff.Add(uid)
					}
					return tt.toggleErr
				},
			)
			swap(t, &testHookMembersWithRole,
				func(
					disgobot.Client, snowflake.ID, discord.Role,
				) (set[snowflake.ID], error) {
					return tt.roleMembers, nil
				},
			)
			swap(t, &testHookMembersInCall,
				func(disgobot.Client, snowflake.ID) set[snowflake.ID] {
					return tt.callMembers
				},
			)

			err := syncVoiceRoles(nil, 0)

			gotErr := fmt.Sprintf("%v", err)
			wantErr := fmt.Sprintf("%v", tt.wantErr)
			if gotErr != wantErr {
				t.Errorf("%s(): %v, want %v",
					funcname(t, syncVoiceRoles), gotErr, wantErr)
			}
			if !cmp.Equal(toggleOn, tt.toggleOn, opts...) {
				t.Errorf("%s(): toggleOn -want +got\n%s",
					funcname(t, syncVoiceRoles),
					cmp.Diff(tt.toggleOn, toggleOn))
			}
			if !cmp.Equal(toggleOff, tt.toggleOff, opts...) {
				t.Errorf("%s(): toggleOff -want +got\n%s",
					funcname(t, syncVoiceRoles),
					cmp.Diff(tt.toggleOff, toggleOff))
			}
		})
	}
}

func funcname(t *testing.T, a any) string {
	t.Helper()
	s := strings.Split(
		runtime.FuncForPC(reflect.ValueOf(a).Pointer()).Name(), ".",
	)
	return s[len(s)-1]
}

func swap[T any](t *testing.T, orig *T, with T) {
	t.Helper()
	o := *orig
	t.Cleanup(func() { *orig = o })
	*orig = with
}
