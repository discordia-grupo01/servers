package channel_service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/model/channelperm"
	"github.com/discordia-grupo01/servers/internal/model/role"
)

// voiceSetup is a voice channel in a server owned by testOwner, seen by a
// member with the given standing and channel overrides.
func voiceSetup(standing role.Standing, layers map[uuid.UUID]channelperm.Layers, channelID uuid.UUID) (*fakeVisibility, func() error) {
	srv := ownerServer()
	channel := model.Channel{ID: channelID, ServerID: srv.ID, Kind: model.ChannelVoice}
	visibility := &fakeVisibility{layers: layers}
	svc := newServiceWithVisibility(
		&fakeServers{server: srv},
		&fakeCategories{},
		&fakeChannels{getByIDChannel: channel},
		&fakeStandings{standing: standing},
		visibility,
	)
	return visibility, func() error {
		_, err := svc.AuthorizeVoice(context.Background(), channel.ID, "member-1")
		return err
	}
}

func TestAuthorizeVoice_ChannelHiddenByOverride_IsNotFound(t *testing.T) {
	channelID := uuid.New()
	_, authorize := voiceSetup(
		role.Standing{IsMember: true, Permissions: role.PermViewChannels | role.PermSendMessages},
		map[uuid.UUID]channelperm.Layers{
			channelID: {Everyone: channelperm.Override{ChannelID: channelID, Deny: role.PermViewChannels}},
		},
		channelID,
	)

	if err := authorize(); !errors.Is(err, model.ErrChannelNotFound) {
		t.Errorf("err = %v, want ErrChannelNotFound", err)
	}
}

func TestAuthorizeVoice_OverrideGrantsViewMissingServerWide_IsAllowed(t *testing.T) {
	channelID := uuid.New()
	_, authorize := voiceSetup(
		role.Standing{IsMember: true},
		map[uuid.UUID]channelperm.Layers{
			channelID: {Roles: []channelperm.Override{{ChannelID: channelID, RoleID: uuid.New(), Allow: role.PermViewChannels}}},
		},
		channelID,
	)

	if err := authorize(); err != nil {
		t.Fatalf("AuthorizeVoice: %v", err)
	}
}

func TestAuthorizeVoice_RoleOverrideAllowsWhatEveryoneDenies_IsAllowed(t *testing.T) {
	channelID := uuid.New()
	_, authorize := voiceSetup(
		role.Standing{IsMember: true, Permissions: role.PermViewChannels},
		map[uuid.UUID]channelperm.Layers{
			channelID: {
				Everyone: channelperm.Override{ChannelID: channelID, Deny: role.PermViewChannels},
				Roles:    []channelperm.Override{{ChannelID: channelID, RoleID: uuid.New(), Allow: role.PermViewChannels}},
			},
		},
		channelID,
	)

	if err := authorize(); err != nil {
		t.Fatalf("AuthorizeVoice: %v", err)
	}
}

func TestAuthorizeVoice_MemberWithoutViewAnywhere_IsNotFound(t *testing.T) {
	_, authorize := voiceSetup(role.Standing{IsMember: true}, nil, uuid.New())

	if err := authorize(); !errors.Is(err, model.ErrChannelNotFound) {
		t.Errorf("err = %v, want ErrChannelNotFound", err)
	}
}

func TestAuthorizeVoice_OverridesOfAnotherChannel_DontApply(t *testing.T) {
	channelID, other := uuid.New(), uuid.New()
	_, authorize := voiceSetup(
		role.Standing{IsMember: true, Permissions: role.PermViewChannels},
		map[uuid.UUID]channelperm.Layers{
			other: {Everyone: channelperm.Override{ChannelID: other, Deny: role.PermViewChannels}},
		},
		channelID,
	)

	if err := authorize(); err != nil {
		t.Fatalf("AuthorizeVoice: %v", err)
	}
}

func TestAuthorizeVoice_Administrator_IgnoresOverridesWithoutLookup(t *testing.T) {
	channelID := uuid.New()
	visibility, authorize := voiceSetup(
		role.Standing{IsMember: true, Permissions: role.PermAdministrator},
		map[uuid.UUID]channelperm.Layers{
			channelID: {Everyone: channelperm.Override{ChannelID: channelID, Deny: role.PermViewChannels}},
		},
		channelID,
	)

	if err := authorize(); err != nil {
		t.Fatalf("AuthorizeVoice: %v", err)
	}
	if visibility.calls != 0 {
		t.Errorf("ChannelLayers calls = %d, want 0", visibility.calls)
	}
}

func TestAuthorizeVoice_NonMember_IsForbiddenWithoutLookup(t *testing.T) {
	visibility, authorize := voiceSetup(role.Standing{}, nil, uuid.New())

	if err := authorize(); !errors.Is(err, model.ErrForbiddenMissingPermission) {
		t.Errorf("err = %v, want ErrForbiddenMissingPermission", err)
	}
	if visibility.calls != 0 {
		t.Errorf("ChannelLayers calls = %d, want 0", visibility.calls)
	}
}

func TestAuthorizeVoice_OverridesLookupErrorIsReturned(t *testing.T) {
	lookupErr := errors.New("overrides unavailable")
	srv := ownerServer()
	channel := model.Channel{ID: uuid.New(), ServerID: srv.ID, Kind: model.ChannelVoice}
	svc := newServiceWithVisibility(
		&fakeServers{server: srv},
		&fakeCategories{},
		&fakeChannels{getByIDChannel: channel},
		&fakeStandings{standing: role.Standing{IsMember: true, Permissions: role.PermViewChannels}},
		&fakeVisibility{err: lookupErr},
	)

	if _, err := svc.AuthorizeVoice(context.Background(), channel.ID, "member-1"); !errors.Is(err, lookupErr) {
		t.Errorf("err = %v, want overrides lookup error", err)
	}
}
