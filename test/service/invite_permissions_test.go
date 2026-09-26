package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/model/role"
	"github.com/discordia-grupo01/servers/internal/service"
	"github.com/discordia-grupo01/servers/internal/service/invitation"
)

// --- RequireCreateInvite / RequireRevokeInvite (pure rules)

func TestRequireCreateInvite(t *testing.T) {
	cases := map[string]struct {
		st      role.Standing
		allowed bool
	}{
		"owner":                           {role.Standing{IsMember: true, IsOwner: true}, true},
		"administrator":                   {role.Standing{IsMember: true, Permissions: role.PermAdministrator}, true},
		"member with CREATE_INVITE":       {role.Standing{IsMember: true, Permissions: role.PermCreateInvite}, true},
		"member without roles":            {role.Standing{IsMember: true}, false},
		"member with other perms":         {role.Standing{IsMember: true, Permissions: role.PermManageChannels | role.PermBanMembers}, false},
		"non member":                      {role.Standing{}, false},
		"non member with stale perm bits": {role.Standing{Permissions: role.PermCreateInvite}, false},
	}
	for name, tc := range cases {
		err := service.RequireCreateInvite(tc.st)
		if tc.allowed && err != nil {
			t.Errorf("%s: unexpected error: %v", name, err)
		}
		if !tc.allowed && !errors.Is(err, model.ErrInvitePermissionDenied) {
			t.Errorf("%s: err = %v, want ErrInvitePermissionDenied", name, err)
		}
	}
}

func TestRequireRevokeInvite(t *testing.T) {
	inv := model.Invitation{CreatedBy: inviterID}
	cases := map[string]struct {
		st      role.Standing
		actor   string
		allowed bool
	}{
		"creator":                          {role.Standing{IsMember: true}, inviterID, true},
		"owner, not creator":               {role.Standing{IsMember: true, IsOwner: true}, "owner", true},
		"administrator, not creator":       {role.Standing{IsMember: true, Permissions: role.PermAdministrator}, "admin", true},
		"member with CREATE_INVITE, other": {role.Standing{IsMember: true, Permissions: role.PermCreateInvite}, "other", false},
		"plain member, other":              {role.Standing{IsMember: true}, "other", false},
		"creator who left the server":      {role.Standing{}, inviterID, false},
		"non member with stale admin bits": {role.Standing{Permissions: role.PermAdministrator}, "ghost", false},
	}
	for name, tc := range cases {
		err := service.RequireRevokeInvite(tc.st, tc.actor, inv)
		if tc.allowed && err != nil {
			t.Errorf("%s: unexpected error: %v", name, err)
		}
		if !tc.allowed && !errors.Is(err, model.ErrInvitePermissionDenied) {
			t.Errorf("%s: err = %v, want ErrInvitePermissionDenied", name, err)
		}
	}
}

// --- GenerateInvitation

func TestGenerate_MemberWithoutCreateInvite_IsDenied(t *testing.T) {
	repo := &fakeInviteRepo{exists: true, member: true, perms: role.PermSendMessages}
	_, err := newInviteService(repo).GenerateInvitation(context.Background(), genParams(uuid.NewString()))
	if !errors.Is(err, model.ErrInvitePermissionDenied) {
		t.Fatalf("err = %v, want ErrInvitePermissionDenied", err)
	}
	if repo.createCalls != 0 {
		t.Error("an invitation was persisted for a member without CREATE_INVITE")
	}
}

func TestGenerate_AdministratorCanInvite(t *testing.T) {
	repo := &fakeInviteRepo{exists: true, member: true, perms: role.PermAdministrator}
	if _, err := newInviteService(repo).GenerateInvitation(context.Background(), genParams(uuid.NewString())); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo.createCalls != 1 {
		t.Errorf("createCalls = %d, want 1", repo.createCalls)
	}
}

func TestGenerate_OwnerCanInviteWithoutRoles(t *testing.T) {
	repo := &fakeInviteRepo{exists: true, ownerID: inviterID}
	if _, err := newInviteService(repo).GenerateInvitation(context.Background(), genParams(uuid.NewString())); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGenerate_StandingErrorPropagates(t *testing.T) {
	boom := errors.New("db down")
	repo := &fakeInviteRepo{exists: true, memberErr: boom}
	_, err := newInviteService(repo).GenerateInvitation(context.Background(), genParams(uuid.NewString()))
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want %v", err, boom)
	}
}

func TestGenerate_ServerLookupErrorPropagates(t *testing.T) {
	boom := errors.New("db down")
	repo := &fakeInviteRepo{existsErr: boom}
	_, err := newInviteService(repo).GenerateInvitation(context.Background(), genParams(uuid.NewString()))
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want %v", err, boom)
	}
}

// --- ListInvitations

func TestList_MemberWithoutCreateInvite_IsDenied(t *testing.T) {
	repo := &fakeInviteRepo{exists: true, member: true}
	_, err := newInviteService(repo).ListInvitations(context.Background(), listParams(uuid.NewString()))
	if !errors.Is(err, model.ErrInvitePermissionDenied) {
		t.Fatalf("err = %v, want ErrInvitePermissionDenied", err)
	}
	if repo.listCalls != 0 {
		t.Error("ListByServer must not be called for a member without CREATE_INVITE")
	}
}

func TestList_AdministratorCanList(t *testing.T) {
	repo := &fakeInviteRepo{exists: true, member: true, perms: role.PermAdministrator}
	if _, err := newInviteService(repo).ListInvitations(context.Background(), listParams(uuid.NewString())); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// --- RevokeInvitation

func revokeAs(repo *fakeInviteRepo, code, userID string) error {
	return newInviteService(repo).RevokeInvitation(context.Background(), invitation.RevokeParams{
		Code: code, UserID: userID,
	})
}

func TestRevoke_OtherMemberCannotRevoke_EvenWithCreateInvite(t *testing.T) {
	inv := activeInvitation(uuid.New())
	repo := revokeSetup(inv, true)
	repo.perms = role.PermCreateInvite

	if err := revokeAs(repo, inv.Code, "someone-else"); !errors.Is(err, model.ErrInvitePermissionDenied) {
		t.Fatalf("err = %v, want ErrInvitePermissionDenied", err)
	}
	if repo.revokeCalls != 0 {
		t.Error("revoke was persisted for a member who is neither admin nor creator")
	}
}

func TestRevoke_AdministratorRevokesAnyInvitation(t *testing.T) {
	inv := activeInvitation(uuid.New())
	repo := revokeSetup(inv, true)
	repo.perms = role.PermAdministrator

	if err := revokeAs(repo, inv.Code, "admin"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo.revokedCode != inv.Code {
		t.Errorf("revoked code = %q, want %q", repo.revokedCode, inv.Code)
	}
}

func TestRevoke_OwnerRevokesAnyInvitation(t *testing.T) {
	inv := activeInvitation(uuid.New())
	repo := revokeSetup(inv, false)
	repo.ownerID = "owner"

	if err := revokeAs(repo, inv.Code, "owner"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo.revokeCalls != 1 {
		t.Errorf("revokeCalls = %d, want 1", repo.revokeCalls)
	}
}

func TestRevoke_CreatorRevokesOwnInvitation_WithoutCreateInvite(t *testing.T) {
	inv := activeInvitation(uuid.New())
	repo := revokeSetup(inv, true) // member, no permissions at all

	if err := revokeAs(repo, inv.Code, inv.CreatedBy); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo.revokeCalls != 1 {
		t.Errorf("revokeCalls = %d, want 1", repo.revokeCalls)
	}
}

func TestRevoke_CreatorWhoLeftCannotRevoke(t *testing.T) {
	inv := activeInvitation(uuid.New())
	repo := revokeSetup(inv, false)

	if err := revokeAs(repo, inv.Code, inv.CreatedBy); !errors.Is(err, model.ErrInvitePermissionDenied) {
		t.Fatalf("err = %v, want ErrInvitePermissionDenied", err)
	}
}
