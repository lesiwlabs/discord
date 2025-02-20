package main

import (
	"fmt"
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

	err := toggleVoiceRole(b, false, &events.GenericGuildVoiceState{})

	if want := `could not get roles: boom`; err == nil {
		t.Errorf("toggleVoiceRole() = <nil>, want %q", want)
	} else if got := err.Error(); got != want {
		t.Errorf("toggleVoiceRole() = %q, want %q", got, want)
	}
}

func TestNoRoles(t *testing.T) {
	b := new(fakeBot)
	b.getRoles = func() (roles []discord.Role, err error) { return }

	err := toggleVoiceRole(b, false, &events.GenericGuildVoiceState{})

	if want := `could not find the role "voice"`; err == nil {
		t.Errorf("toggleVoiceRole() = <nil>, want %q", want)
	} else if got := err.Error(); got != want {
		t.Errorf("toggleVoiceRole() = %q, want %q", got, want)
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

	err := toggleVoiceRole(b, false, &events.GenericGuildVoiceState{})

	if want := `could not find the role "voice"`; err == nil {
		t.Errorf("toggleVoiceRole() = <nil>, want %q", want)
	} else if got := err.Error(); got != want {
		t.Errorf("toggleVoiceRole() = %q, want %q", got, want)
	}
}

func TestRemoveMemberRoleFail(t *testing.T) {
	b := new(fakeBot)
	role := discord.Role{Name: "voice"}
	b.getRoles = func() ([]discord.Role, error) {
		return []discord.Role{role, {Name: "someotherrole"}}, nil
	}
	b.toggleRoleErr = fmt.Errorf("boom")

	err := toggleVoiceRole(b, false, &events.GenericGuildVoiceState{})

	if want := `failed to toggle role (apply = false): boom`; err == nil {
		t.Errorf("toggleVoiceRole() = <nil>, want %q", want)
	} else if got := err.Error(); got != want {
		t.Errorf("toggleVoiceRole() = %q, want %q", got, want)
	}
}

func TestRemoveMemberRole(t *testing.T) {
	b := new(fakeBot)
	role := discord.Role{Name: "voice", ID: snowflake.ID(3)}
	b.getRoles = func() ([]discord.Role, error) {
		return []discord.Role{role, {Name: "someotherrole"}}, nil
	}

	err := toggleVoiceRole(b, false, &events.GenericGuildVoiceState{
		VoiceState: discord.VoiceState{GuildID: snowflake.ID(1)},
		Member:     discord.Member{User: discord.User{ID: snowflake.ID(2)}},
	})

	if err != nil {
		t.Errorf("toggleVoiceRole() = %q, want <nil>", err.Error())
	}
	wantCalls := []toggleRole{
		{snowflake.ID(1), snowflake.ID(2), snowflake.ID(3)},
	}
	if got, want := b.removeMemberRole, wantCalls; !cmp.Equal(got, want) {
		t.Errorf("RemoveMemberRole calls: -want +got\n%s", cmp.Diff(got, want))
	}
	if got, want := len(b.addMemberRole), 0; got != want {
		t.Errorf("got %d AddMemberRole calls, want %d", got, want)
	}
}

func TestAddMemberRole(t *testing.T) {
	b := new(fakeBot)
	role := discord.Role{Name: "voice", ID: snowflake.ID(3)}
	b.getRoles = func() ([]discord.Role, error) {
		return []discord.Role{role, {Name: "someotherrole"}}, nil
	}

	err := toggleVoiceRole(b, true, &events.GenericGuildVoiceState{
		VoiceState: discord.VoiceState{GuildID: snowflake.ID(1)},
		Member:     discord.Member{User: discord.User{ID: snowflake.ID(2)}},
	})

	if err != nil {
		t.Errorf("toggleVoiceRole() = %q, want <nil>", err.Error())
	}
	wantCalls := []toggleRole{
		{snowflake.ID(1), snowflake.ID(2), snowflake.ID(3)},
	}
	if got, want := b.addMemberRole, wantCalls; !cmp.Equal(got, want) {
		t.Errorf("AddMemberRole calls: -want +got\n%s", cmp.Diff(got, want))
	}
	if got, want := len(b.removeMemberRole), 0; got != want {
		t.Errorf("got %d RemoveMemberRole calls, want %d", got, want)
	}
}
