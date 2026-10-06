package channelperm_service_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/model/channelperm"
	"github.com/discordia-grupo01/servers/internal/model/role"
	"github.com/discordia-grupo01/servers/internal/service/channelperm_service"
)

func names(ss ...string) []string { return ss }

// #57 CA1: a manager makes #anuncios read-only for @everyone. The override is
// stored for that channel and role only.
func TestSetOverride_StoresTheOverride(t *testing.T) {
	f := newFixture()
	o, err := f.set(managerID, f.everyone.ID, nil, names("SEND_MESSAGES"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if o.ChannelID != f.channel.ID || o.RoleID != f.everyone.ID || o.Allow != 0 || o.Deny != role.PermSendMessages || o.UpdatedAt.IsZero() {
		t.Errorf("override = %+v", o)
	}
	if f.repo.upsertCalls != 1 || f.repo.upserted.Deny != role.PermSendMessages {
		t.Errorf("repo got %+v after %d calls", f.repo.upserted, f.repo.upsertCalls)
	}
}

// Both lists empty: the override is removed instead of stored, idempotently.
func TestSetOverride_EmptyListsRemoveTheOverride(t *testing.T) {
	for _, found := range []bool{true, false} {
		f := newFixture()
		f.repo.deleteFound = found
		o, err := f.set(managerID, f.low.ID, nil, []string{})
		if err != nil {
			t.Fatalf("found=%v: unexpected error %v", found, err)
		}
		if !o.IsEmpty() || o.RoleID != f.low.ID || f.repo.deleteCalls != 1 || f.repo.upsertCalls != 0 {
			t.Errorf("found=%v: override=%+v delete=%d upsert=%d", found, o, f.repo.deleteCalls, f.repo.upsertCalls)
		}
	}
}

// CA2: without MANAGE_ROLES (nor admin) nothing can be set, removed or
// listed.
func TestOverrides_WithoutManageRoles_Forbidden(t *testing.T) {
	for name, actor := range map[string]string{"plain member": memberID, "not a member": "stranger"} {
		f := newFixture()
		if _, err := f.set(actor, f.low.ID, nil, names("SEND_MESSAGES")); !errors.Is(err, model.ErrForbiddenNotAdmin) {
			t.Errorf("%s set: err = %v", name, err)
		}
		if err := f.del(actor, f.low.ID); !errors.Is(err, model.ErrForbiddenNotAdmin) {
			t.Errorf("%s delete: err = %v", name, err)
		}
		_, err := f.svc.ListOverrides(context.Background(), channelperm_service.ListOverridesParams{ChannelID: f.channel.ID, ActorUserID: actor})
		if !errors.Is(err, model.ErrForbiddenNotAdmin) {
			t.Errorf("%s list: err = %v", name, err)
		}
		if f.repo.upsertCalls+f.repo.deleteCalls != 0 {
			t.Errorf("%s: repo written", name)
		}
	}
}

func TestOverrides_OwnerAndAdministratorsCanManageAnyRole(t *testing.T) {
	f := newFixture()
	if _, err := f.set(ownerID, f.mod.ID, names("MANAGE_MESSAGES"), nil); err != nil {
		t.Errorf("owner on a role above everyone: %v", err)
	}
	f = newFixture()
	f.members.byUser["admin-1"] = member(rank(2), role.PermAdministrator)
	if _, err := f.set("admin-1", f.low.ID, names("MANAGE_MESSAGES"), nil); err != nil {
		t.Errorf("administrator: %v", err)
	}
}

// Hierarchy: not on a role at or above your own -- but @everyone is always
// below.
func TestOverrides_RoleAboveOwn_Forbidden(t *testing.T) {
	f := newFixture()
	if _, err := f.set(managerID, f.mod.ID, nil, names("SEND_MESSAGES")); !errors.Is(err, model.ErrRoleAboveOwn) {
		t.Errorf("set on a higher role: err = %v", err)
	}
	if err := f.del(managerID, f.mod.ID); !errors.Is(err, model.ErrRoleAboveOwn) {
		t.Errorf("delete on a higher role: err = %v", err)
	}
	f.repo.deleteFound = true
	if _, err := f.set(managerID, f.everyone.ID, nil, names("VIEW_CHANNELS")); err != nil {
		t.Errorf("@everyone: unexpected error %v", err)
	}
	if _, err := f.set(managerID, f.low.ID, nil, names("VIEW_CHANNELS")); err != nil {
		t.Errorf("lower role: unexpected error %v", err)
	}
}

// Escalation: allowing a permission the actor doesn't hold is rejected with
// the exact list; denying it is fine.
func TestSetOverride_CannotAllowWhatYouDontHold(t *testing.T) {
	f := newFixture()
	_, err := f.set(managerID, f.low.ID, names("MANAGE_MESSAGES", "SEND_MESSAGES"), nil)
	var pnh *model.PermissionNotHeldError
	if !errors.As(err, &pnh) || !reflect.DeepEqual(pnh.Permissions, []string{"MANAGE_MESSAGES"}) {
		t.Fatalf("err = %v", err)
	}
	if f.repo.upsertCalls != 0 {
		t.Error("repo written")
	}
	if _, err := f.set(managerID, f.low.ID, nil, names("MANAGE_MESSAGES")); err != nil {
		t.Errorf("deny: unexpected error %v", err)
	}
}

func TestSetOverride_InvalidPermissions(t *testing.T) {
	cases := map[string]struct {
		allow, deny []string
		want        error
	}{
		"unknown name":           {names("FLY"), nil, model.ErrInvalidPermission},
		"server-level":           {nil, names("KICK_MEMBERS"), model.ErrPermissionNotOverridable},
		"allowed and denied":     {names("SEND_MESSAGES"), names("SEND_MESSAGES"), model.ErrPermissionConflict},
		"administrator override": {names("ADMINISTRATOR"), nil, model.ErrPermissionNotOverridable},
	}
	for name, tc := range cases {
		f := newFixture()
		if _, err := f.set(ownerID, f.low.ID, tc.allow, tc.deny); !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want %v", name, err, tc.want)
		}
		if f.repo.upsertCalls+f.repo.deleteCalls != 0 {
			t.Errorf("%s: repo written", name)
		}
	}
}

func TestOverrides_LookupErrors(t *testing.T) {
	f := newFixture()
	if _, err := f.set(ownerID, f.otherServerRole.ID, nil, names("SEND_MESSAGES")); !errors.Is(err, model.ErrRoleServerMismatch) {
		t.Errorf("role of another server: err = %v", err)
	}
	if _, err := f.set(ownerID, uuid.New(), nil, names("SEND_MESSAGES")); !errors.Is(err, model.ErrRoleNotFound) {
		t.Errorf("unknown role: err = %v", err)
	}
	_, err := f.svc.SetOverride(context.Background(), channelperm_service.SetOverrideParams{ChannelID: uuid.New(), RoleID: f.low.ID, ActorUserID: ownerID})
	if !errors.Is(err, model.ErrChannelNotFound) {
		t.Errorf("unknown channel: err = %v", err)
	}
	if _, err := f.set("", f.low.ID, nil, nil); !errors.Is(err, model.ErrUnauthorized) {
		t.Errorf("no identity: err = %v", err)
	}
}

// Without MANAGE_ROLES the role isn't even looked up: a caller can't probe
// which role ids exist.
func TestSetOverride_PermissionIsCheckedBeforeTheRole(t *testing.T) {
	f := newFixture()
	if _, err := f.set(memberID, uuid.New(), nil, nil); !errors.Is(err, model.ErrForbiddenNotAdmin) {
		t.Fatalf("err = %v, want ErrForbiddenNotAdmin", err)
	}
}

// The checks are repeated inside the transaction: a manager who lost
// MANAGE_ROLES (or was demoted below the role) in between is rejected.
func TestOverrides_RecheckedInsideTheTransaction(t *testing.T) {
	f := newFixture()
	lost := member(rank(2), role.PermViewChannels)
	f.repo.txActor = &lost
	if _, err := f.set(managerID, f.low.ID, nil, names("SEND_MESSAGES")); !errors.Is(err, model.ErrForbiddenNotAdmin) {
		t.Errorf("set: err = %v", err)
	}
	if f.repo.upserted.RoleID != uuid.Nil {
		t.Error("set: the override was stored")
	}

	f = newFixture()
	demoted := member(rank(5), role.PermManageRoles)
	f.repo.txActor = &demoted
	if err := f.del(managerID, f.low.ID); !errors.Is(err, model.ErrRoleAboveOwn) {
		t.Errorf("delete: err = %v", err)
	}
}

func TestDeleteOverride(t *testing.T) {
	f := newFixture()
	f.repo.deleteFound = true
	if err := f.del(managerID, f.low.ID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	f = newFixture()
	if err := f.del(managerID, f.low.ID); !errors.Is(err, model.ErrChannelOverrideNotFound) {
		t.Errorf("no override: err = %v", err)
	}
}

func TestListOverrides(t *testing.T) {
	f := newFixture()
	f.repo.list = []channelperm.Override{{ChannelID: f.channel.ID, RoleID: f.everyone.ID, Deny: role.PermSendMessages}}
	got, err := f.svc.ListOverrides(context.Background(), channelperm_service.ListOverridesParams{ChannelID: f.channel.ID, ActorUserID: managerID})
	if err != nil || len(got) != 1 || got[0].RoleID != f.everyone.ID {
		t.Fatalf("got %+v, err %v", got, err)
	}
}

// #57 CA1 seen from a member: the @everyone deny takes SEND_MESSAGES away in
// this channel.
func TestEffectivePermissions_AppliesTheOverrides(t *testing.T) {
	f := newFixture()
	f.repo.everyone = channelperm.Override{RoleID: f.everyone.ID, Deny: role.PermSendMessages}
	got, err := f.effective(memberID, memberID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(got, []string{"VIEW_CHANNELS"}) {
		t.Errorf("permissions = %v, want [VIEW_CHANNELS]", got)
	}
}

// Owner and administrators always get everything, without loading overrides.
func TestEffectivePermissions_AdminsGetEverything(t *testing.T) {
	f := newFixture()
	f.members.byUser["admin-1"] = member(rank(3), role.PermAdministrator)
	f.repo.everyone = channelperm.Override{Deny: role.PermViewChannels | role.PermSendMessages}
	for _, target := range []string{ownerID, "admin-1"} {
		got, err := f.effective(memberID, target)
		if err != nil || !reflect.DeepEqual(got, role.PermissionNames(role.AllPermissions)) {
			t.Errorf("%s: got %v, err %v", target, got, err)
		}
	}
	if f.repo.forCalls != 0 {
		t.Error("overrides were loaded for an administrator")
	}
}

func TestEffectivePermissions_AccessRules(t *testing.T) {
	f := newFixture()
	if _, err := f.effective("stranger", memberID); !errors.Is(err, model.ErrForbiddenNotMember) {
		t.Errorf("caller not a member: err = %v", err)
	}
	if _, err := f.effective(memberID, "stranger"); !errors.Is(err, model.ErrMemberNotFound) {
		t.Errorf("target not a member: err = %v", err)
	}
	if _, err := f.effective("", memberID); !errors.Is(err, model.ErrUnauthorized) {
		t.Errorf("no identity: err = %v", err)
	}
	_, err := f.svc.EffectivePermissions(context.Background(), channelperm_service.EffectivePermissionsParams{ChannelID: uuid.New(), ActorUserID: memberID, TargetUserID: memberID})
	if !errors.Is(err, model.ErrChannelNotFound) {
		t.Errorf("unknown channel: err = %v", err)
	}
	boom := errors.New("db down")
	f.repo.forMemberErr = boom
	if _, err := f.effective(memberID, memberID); !errors.Is(err, boom) {
		t.Errorf("repo error: err = %v", err)
	}
}

func TestServiceErrorsPropagate(t *testing.T) {
	boom := errors.New("db down")
	f := newFixture()
	f.members.err = boom
	if _, err := f.set(managerID, f.low.ID, nil, names("SEND_MESSAGES")); !errors.Is(err, boom) {
		t.Errorf("standing error: err = %v", err)
	}
	f = newFixture()
	f.repo.upsertErr = boom
	if _, err := f.set(managerID, f.low.ID, nil, names("SEND_MESSAGES")); !errors.Is(err, boom) {
		t.Errorf("upsert error: err = %v", err)
	}
	f = newFixture()
	f.repo.deleteErr = boom
	if err := f.del(managerID, f.low.ID); !errors.Is(err, boom) {
		t.Errorf("delete error: err = %v", err)
	}
	svc := channelperm_service.New(&fakeServers{err: model.ErrServerNotFound}, &fakeChannels{byID: map[uuid.UUID]model.Channel{f.channel.ID: f.channel}}, &fakeRoles{}, &fakeMembers{}, &fakeRepo{})
	if _, err := svc.ListOverrides(context.Background(), channelperm_service.ListOverridesParams{ChannelID: f.channel.ID, ActorUserID: ownerID}); !errors.Is(err, model.ErrServerNotFound) {
		t.Errorf("server not found: err = %v", err)
	}
}
