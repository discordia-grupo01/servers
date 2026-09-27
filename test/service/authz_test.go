package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/model/role"
	"github.com/discordia-grupo01/servers/internal/service"
)

// Every "manage X" capability shares the same rule: the owner, a member
// holding ADMINISTRATOR, or a member whose roles grant the specific
// permission for that capability
var manageChecks = map[string]func(role.Standing) error{
	"RequireManageRoles":    service.RequireManageRoles,
	"RequireManageChannels": service.RequireManageChannels,
	"RequireManageServer":   service.RequireManageServer,
}

var manageChecksPermission = map[string]role.Permission{
	"RequireManageRoles":    role.PermManageRoles,
	"RequireManageChannels": role.PermManageChannels,
	"RequireManageServer":   role.PermManageServer,
}

func TestRequireManage_OwnerAllowed(t *testing.T) {
	for name, check := range manageChecks {
		if err := check(role.Standing{IsMember: true, IsOwner: true}); err != nil {
			t.Errorf("%s: unexpected error for the owner: %v", name, err)
		}
	}
}

func TestRequireManage_AdministratorAllowed(t *testing.T) {
	st := role.Standing{IsMember: true, Permissions: role.PermAdministrator | role.PermSendMessages}
	for name, check := range manageChecks {
		if err := check(st); err != nil {
			t.Errorf("%s: unexpected error for an administrator: %v", name, err)
		}
	}
}

func TestRequireManage_WithoutRelevantPermission_Forbidden(t *testing.T) {
	cases := map[string]role.Standing{
		"non member":                  {},
		"member without roles":        {IsMember: true},
		"member with unrelated perms": {IsMember: true, Permissions: role.PermSendMessages | role.PermCreateInvite},
	}
	for checkName, check := range manageChecks {
		for caseName, st := range cases {
			if err := check(st); !errors.Is(err, model.ErrForbiddenNotAdmin) {
				t.Errorf("%s / %s: err = %v, want ErrForbiddenNotAdmin", checkName, caseName, err)
			}
		}
	}
}

func TestRequireManage_GranularPermissionAlone_Allowed(t *testing.T) {
	for name, check := range manageChecks {
		st := role.Standing{IsMember: true, Permissions: manageChecksPermission[name]}
		if err := check(st); err != nil {
			t.Errorf("%s: unexpected error for a member holding only its own permission: %v", name, err)
		}
	}
}

func TestRequireManage_OtherManagePermissionAlone_Forbidden(t *testing.T) {
	for checkName, check := range manageChecks {
		for otherName, otherPerm := range manageChecksPermission {
			if otherName == checkName {
				continue
			}
			st := role.Standing{IsMember: true, Permissions: otherPerm}
			if err := check(st); !errors.Is(err, model.ErrForbiddenNotAdmin) {
				t.Errorf("%s with only %s: err = %v, want ErrForbiddenNotAdmin", checkName, otherName, err)
			}
		}
	}
}

type fixedStandings struct {
	standing role.Standing
	err      error
	calls    int
}

func (f *fixedStandings) MemberStanding(_ context.Context, _ uuid.UUID, _ string) (role.Standing, error) {
	f.calls++
	return f.standing, f.err
}

func TestResolveStanding_OwnerShortCircuits(t *testing.T) {
	reader := &fixedStandings{}
	srv := model.Server{ID: uuid.New(), OwnerID: testOwner}

	st, err := service.ResolveStanding(context.Background(), reader, srv, testOwner)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !st.IsOwner || !st.IsMember || !st.IsAdmin() {
		t.Errorf("standing = %+v, want owner", st)
	}
	if reader.calls != 0 {
		t.Errorf("MemberStanding calls = %d, want 0 for the owner", reader.calls)
	}
}

func TestResolveStanding_MemberComesFromReader(t *testing.T) {
	// Even if the reader claimed IsOwner, only the server row decides it.
	reader := &fixedStandings{standing: role.Standing{IsMember: true, IsOwner: true, Permissions: role.PermBanMembers}}
	srv := model.Server{ID: uuid.New(), OwnerID: testOwner}

	st, err := service.ResolveStanding(context.Background(), reader, srv, "member-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if st.IsOwner || !st.IsMember || st.Permissions != role.PermBanMembers {
		t.Errorf("standing = %+v", st)
	}
}

func TestResolveStanding_EmptyUserIsNobody(t *testing.T) {
	reader := &fixedStandings{}
	st, err := service.ResolveStanding(context.Background(), reader, model.Server{OwnerID: testOwner}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if st.IsMember || st.IsAdmin() || reader.calls != 0 {
		t.Errorf("standing = %+v, calls = %d", st, reader.calls)
	}
}

func TestResolveStanding_ReaderErrorPropagates(t *testing.T) {
	boom := errors.New("db down")
	_, err := service.ResolveStanding(context.Background(), &fixedStandings{err: boom}, model.Server{OwnerID: testOwner}, "member-1")
	if !errors.Is(err, boom) {
		t.Errorf("err = %v, want %v", err, boom)
	}
}
