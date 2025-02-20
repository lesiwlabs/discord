package main

import (
	"fmt"
	"math/rand/v2"
	"testing"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/disgo/rest"
	"github.com/disgoorg/snowflake/v2"
	"github.com/google/go-cmp/cmp"
)

type fakeBot struct {
	rest.Rest        // Fulfill the rest.Rest interface.
	addMemberRole    []toggleRole
	removeMemberRole []toggleRole
	getRoles         func() ([]discord.Role, error)
	toggleRoleErr    error
}
type toggleRole struct{ GuildID, UserID, RoleID snowflake.ID }

func (b *fakeBot) AddMemberRole(
	guild, user, role snowflake.ID, _ ...rest.RequestOpt,
) error {
	b.addMemberRole = append(b.addMemberRole, toggleRole{guild, user, role})
	return b.toggleRoleErr
}

func (b *fakeBot) RemoveMemberRole(
	guild, user, role snowflake.ID, _ ...rest.RequestOpt,
) error {
	b.removeMemberRole = append(b.removeMemberRole,
		toggleRole{guild, user, role})
	return b.toggleRoleErr
}

func (b *fakeBot) GetRoles(
	snowflake.ID, ...rest.RequestOpt,
) ([]discord.Role, error) {
	return b.getRoles()
}

func TestRolesError(t *testing.T) {
	b := new(fakeBot)
	b.getRoles = func() ([]discord.Role, error) {
		return nil, fmt.Errorf("boom")
	}

	_, err := voiceRole(b, 0)

	if want := `could not get roles: boom`; err == nil {
		t.Errorf("voiceRole() = _, <nil>, want %q", want)
	} else if got := err.Error(); got != want {
		t.Errorf("voiceRole() = _, %q, want %q", got, want)
	}
}

func TestNoRoles(t *testing.T) {
	b := new(fakeBot)
	b.getRoles = func() (roles []discord.Role, err error) { return }

	_, err := voiceRole(b, 0)

	if want := `could not find the role "voice"`; err == nil {
		t.Errorf("voiceRole() = _, <nil>, want %q", want)
	} else if got := err.Error(); got != want {
		t.Errorf("voiceRole() = _, %q, want %q", got, want)
	}
}

func TestNoVoiceRole(t *testing.T) {
	b := new(fakeBot)
	b.getRoles = func() ([]discord.Role, error) {
		return []discord.Role{
			{Name: "notvoice"},
			{Name: ""},
		}, nil
	}

	_, err := voiceRole(b, 0)

	if want := `could not find the role "voice"`; err == nil {
		t.Errorf("voiceRole() = _, <nil>, want %q", want)
	} else if got := err.Error(); got != want {
		t.Errorf("voiceRole() = _, %q, want %q", got, want)
	}
}

func TestVoiceRoleFound(t *testing.T) {
	b := new(fakeBot)
	role := discord.Role{Name: "voice"}
	b.getRoles = func() ([]discord.Role, error) {
		return []discord.Role{
			{Name: "notvoice"},
			role,
			{Name: ""},
		}, nil
	}

	foundRole, err := voiceRole(b, 0)

	if err != nil {
		t.Errorf("voiceRole() = _, %q, want _, <nil>", err.Error())
	} else if got, want := foundRole, role; got != want {
		t.Errorf("voiceRole() = %#v, <nil>, want %#v, <nil>", got, want)
	}
}

func mockVoiceRole(t *testing.T) discord.Role {
	role := discord.Role{Name: "voice", ID: snowflake.ID(rand.Uint64())}
	swap(t, &testHookVoiceRole,
		func(rest.Rest, snowflake.ID) (discord.Role, error) {
			return role, nil
		},
	)
	return role
}

func TestRemoveMemberRoleFail(t *testing.T) {
	b := new(fakeBot)
	b.toggleRoleErr = fmt.Errorf("boom")
	mockVoiceRole(t)

	err := toggleVoiceRole(b, false, &events.GenericGuildVoiceState{})

	if want := `failed to toggle role (apply = false): boom`; err == nil {
		t.Errorf("toggleVoiceRole() = <nil>, want %q", want)
	} else if got := err.Error(); got != want {
		t.Errorf("toggleVoiceRole() = %q, want %q", got, want)
	}
}

func TestRemoveMemberRole(t *testing.T) {
	b := new(fakeBot)
	role := mockVoiceRole(t)

	err := toggleVoiceRole(b, false, &events.GenericGuildVoiceState{
		VoiceState: discord.VoiceState{GuildID: 1},
		Member:     discord.Member{User: discord.User{ID: 2}},
	})

	if err != nil {
		t.Errorf("toggleVoiceRole() = %q, want <nil>", err.Error())
	}
	wantCalls := []toggleRole{{1, 2, role.ID}}
	if got, want := b.removeMemberRole, wantCalls; !cmp.Equal(got, want) {
		t.Errorf("RemoveMemberRole calls: -want +got\n%s", cmp.Diff(got, want))
	}
	if got, want := len(b.addMemberRole), 0; got != want {
		t.Errorf("got %d AddMemberRole calls, want %d", got, want)
	}
}

func TestAddMemberRole(t *testing.T) {
	b := new(fakeBot)
	role := mockVoiceRole(t)

	err := toggleVoiceRole(b, true, &events.GenericGuildVoiceState{
		VoiceState: discord.VoiceState{GuildID: snowflake.ID(1)},
		Member:     discord.Member{User: discord.User{ID: snowflake.ID(2)}},
	})

	if err != nil {
		t.Errorf("toggleVoiceRole() = %q, want <nil>", err.Error())
	}
	wantCalls := []toggleRole{{1, 2, role.ID}}
	if got, want := b.addMemberRole, wantCalls; !cmp.Equal(got, want) {
		t.Errorf("AddMemberRole calls: -want +got\n%s", cmp.Diff(got, want))
	}
	if got, want := len(b.removeMemberRole), 0; got != want {
		t.Errorf("got %d RemoveMemberRole calls, want %d", got, want)
	}
}

func swap[T any](t *testing.T, orig *T, with T) {
	t.Helper()
	o := *orig
	t.Cleanup(func() { *orig = o })
	*orig = with
}
