package role_service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/model/role"
	"github.com/discordia-grupo01/servers/internal/service/role_service"
)

// threeRoles is a server hierarchy A(1) B(2) C(3).
func threeRoles(serverID uuid.UUID) (roles []role.Role, a, b, c uuid.UUID) {
	a, b, c = uuid.New(), uuid.New(), uuid.New()
	return []role.Role{
		{ID: a, ServerID: serverID, Name: "A", Position: 1},
		{ID: b, ServerID: serverID, Name: "B", Position: 2},
		{ID: c, ServerID: serverID, Name: "C", Position: 3},
	}, a, b, c
}

func reorderAs(svc *role_service.RoleService, serverID uuid.UUID, userID string, ids ...uuid.UUID) ([]role.Role, error) {
	return svc.ReorderRoles(context.Background(), role_service.ReorderRolesParams{
		ServerID: serverID, UserID: userID, RoleIDs: ids,
	})
}

func adminAt(position int) role.Standing {
	st := adminStanding()
	st.TopPosition = &position
	return st
}

func TestReorderRoles_OwnerReordersAnything(t *testing.T) {
	srv := ownerServer()
	current, a, b, c := threeRoles(srv.ID)
	roles := &fakeRoles{listRoles: current}
	svc := newService(&fakeServers{server: srv}, roles)

	got, err := reorderAs(svc, srv.ID, testOwner, c, b, a)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 3 || got[0].ID != c || got[0].Position != 1 || got[2].ID != a || got[2].Position != 3 {
		t.Errorf("got = %+v, want C(1) B(2) A(3)", got)
	}
}

func TestReorderRoles_AdminRearrangesRolesBelowTheirOwn(t *testing.T) {
	srv := ownerServer()
	current, a, b, c := threeRoles(srv.ID)
	roles := &fakeRoles{listRoles: current}
	svc := newServiceWithMembers(&fakeServers{server: srv}, roles, &fakeMembers{standing: adminAt(1)})

	if _, err := reorderAs(svc, srv.ID, testAdmin, a, c, b); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(roles.reorderWritten) != 3 || roles.reorderWritten[1] != c {
		t.Errorf("written = %v", roles.reorderWritten)
	}
}

// CA2: a non-owner administrator can't reorder a role at or above their own.
func TestReorderRoles_AdminCannotTouchRolesAtOrAboveTheirOwn(t *testing.T) {
	srv := ownerServer()
	current, a, b, c := threeRoles(srv.ID)
	// The admin's highest role is B (position 2).
	cases := map[string][]uuid.UUID{
		"moves their own role (C jumps over B)": {a, c, b},
		"moves a role above their own":          {b, a, c},
		"lifts a lower role to the very top":    {c, a, b},
	}
	for name, order := range cases {
		roles := &fakeRoles{listRoles: current}
		svc := newServiceWithMembers(&fakeServers{server: srv}, roles, &fakeMembers{standing: adminAt(2)})
		_, err := reorderAs(svc, srv.ID, testAdmin, order...)
		if !errors.Is(err, model.ErrRoleReorderAboveOwn) {
			t.Errorf("%s: err = %v, want ErrRoleReorderAboveOwn", name, err)
		}
		if roles.reorderWritten != nil {
			t.Errorf("%s: an order was written", name)
		}
	}
}

func TestReorderRoles_NonAdminForbidden(t *testing.T) {
	srv := ownerServer()
	current, a, b, c := threeRoles(srv.ID)
	roles := &fakeRoles{listRoles: current}
	members := &fakeMembers{standing: role.Standing{IsMember: true, Permissions: role.PermManageRoles}}
	svc := newServiceWithMembers(&fakeServers{server: srv}, roles, members)

	_, err := reorderAs(svc, srv.ID, "member-1", c, b, a)
	if !errors.Is(err, model.ErrForbiddenNotAdmin) {
		t.Fatalf("err = %v, want ErrForbiddenNotAdmin", err)
	}
	if roles.reorderCalls != 0 {
		t.Error("the repository was called for a non-administrator")
	}
}

func TestReorderRoles_ListMustMatchTheServerRolesExactly(t *testing.T) {
	srv := ownerServer()
	current, a, b, c := threeRoles(srv.ID)
	cases := map[string][]uuid.UUID{
		"missing a role":        {a, b},
		"extra role":            {a, b, c, uuid.New()},
		"repeated role":         {a, b, b},
		"role of another place": {a, b, uuid.New()},
		"empty list":            {},
	}
	for name, order := range cases {
		roles := &fakeRoles{listRoles: current}
		svc := newService(&fakeServers{server: srv}, roles)
		_, err := reorderAs(svc, srv.ID, testOwner, order...)
		if !errors.Is(err, model.ErrRoleReorderInvalid) {
			t.Errorf("%s: err = %v, want ErrRoleReorderInvalid", name, err)
		}
		if roles.reorderWritten != nil {
			t.Errorf("%s: an order was written", name)
		}
	}
}

func TestReorderRoles_ServerWithoutRoles_EmptyListIsFine(t *testing.T) {
	srv := ownerServer()
	svc := newService(&fakeServers{server: srv}, &fakeRoles{})
	got, err := reorderAs(svc, srv.ID, testOwner)
	if err != nil || len(got) != 0 {
		t.Fatalf("got %v, %v; want empty, nil", got, err)
	}
}

func TestReorderRoles_ServerNotFound(t *testing.T) {
	svc := newService(&fakeServers{getErr: model.ErrServerNotFound}, &fakeRoles{})
	_, err := reorderAs(svc, uuid.New(), testOwner)
	if !errors.Is(err, model.ErrServerNotFound) {
		t.Fatalf("err = %v, want ErrServerNotFound", err)
	}
}

func TestReorderRoles_RepoErrorPropagates(t *testing.T) {
	srv := ownerServer()
	boom := errors.New("db down")
	svc := newService(&fakeServers{server: srv}, &fakeRoles{reorderErr: boom})
	_, err := reorderAs(svc, srv.ID, testOwner)
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want %v", err, boom)
	}
}

// The decision uses the actor's rank as it is at write time: if the admin was
// moved down (or lost ADMINISTRATOR) after the service's first check, the
// check inside the transaction rejects the reorder and nothing is written.
func TestReorderRoles_RechecksTheActorInsideTheTransaction(t *testing.T) {
	srv := ownerServer()
	current, a, b, c := threeRoles(srv.ID)

	demoted := adminAt(3)
	roles := &fakeRoles{listRoles: current, txStanding: &demoted}
	svc := newServiceWithMembers(&fakeServers{server: srv}, roles, &fakeMembers{standing: adminAt(1)})
	if _, err := reorderAs(svc, srv.ID, testAdmin, a, c, b); !errors.Is(err, model.ErrRoleReorderAboveOwn) {
		t.Errorf("demoted admin: err = %v, want ErrRoleReorderAboveOwn", err)
	}
	if roles.reorderWritten != nil {
		t.Errorf("demoted admin: positions written: %v", roles.reorderWritten)
	}

	noLongerAdmin := role.Standing{IsMember: true}
	roles = &fakeRoles{listRoles: current, txStanding: &noLongerAdmin}
	svc = newServiceWithMembers(&fakeServers{server: srv}, roles, &fakeMembers{standing: adminAt(1)})
	if _, err := reorderAs(svc, srv.ID, testAdmin, a, c, b); !errors.Is(err, model.ErrForbiddenNotAdmin) {
		t.Errorf("admin lost ADMINISTRATOR: err = %v, want ErrForbiddenNotAdmin", err)
	}
}
