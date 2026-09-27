package role_test

import (
	"testing"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model/role"
)

func pos(n int) *int { return &n }

func TestOutranks(t *testing.T) {
	owner := role.Standing{IsMember: true, IsOwner: true}
	top := role.Standing{IsMember: true, TopPosition: pos(1)}
	mid := role.Standing{IsMember: true, TopPosition: pos(2)}
	mid2 := role.Standing{IsMember: true, TopPosition: pos(2)}
	noRoles := role.Standing{IsMember: true}

	cases := []struct {
		name string
		a, b role.Standing
		want bool
	}{
		{"owner over a top role", owner, top, true},
		{"owner over a member without roles", owner, noRoles, true},
		{"nobody outranks the owner", top, owner, false},
		{"owner does not outrank another owner", owner, owner, false},
		{"higher role (lower position) wins", top, mid, true},
		{"lower role loses", mid, top, false},
		{"same rank does not outrank", mid, mid2, false},
		{"any role over no roles", mid, noRoles, true},
		{"no roles never outranks", noRoles, mid, false},
		{"two members without roles", noRoles, noRoles, false},
	}
	for _, tc := range cases {
		if got := tc.a.Outranks(tc.b); got != tc.want {
			t.Errorf("%s: Outranks = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// Four roles, highest first: A(1) B(2) C(3) D(4).
func hierarchy() ([]role.Role, uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID) {
	a, b, c, d := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	return []role.Role{
		{ID: a, Position: 1}, {ID: b, Position: 2}, {ID: c, Position: 3}, {ID: d, Position: 4},
	}, a, b, c, d
}

func TestCanReorder(t *testing.T) {
	current, a, b, c, d := hierarchy()
	owner := role.Standing{IsMember: true, IsOwner: true}
	adminAtB := role.Standing{IsMember: true, Permissions: role.PermAdministrator, TopPosition: pos(2)}
	adminNoRoles := role.Standing{IsMember: true, Permissions: role.PermAdministrator}

	cases := []struct {
		name     string
		actor    role.Standing
		newOrder []uuid.UUID
		want     bool
	}{
		{"owner reverses everything", owner, []uuid.UUID{d, c, b, a}, true},
		{"admin swaps two roles below their own", adminAtB, []uuid.UUID{a, b, d, c}, true},
		{"admin leaves the order unchanged", adminAtB, []uuid.UUID{a, b, c, d}, true},
		{"admin moves their own role down", adminAtB, []uuid.UUID{a, c, b, d}, false},
		{"admin moves a role above their own", adminAtB, []uuid.UUID{b, a, c, d}, false},
		{"admin lifts a lower role above their own", adminAtB, []uuid.UUID{a, d, b, c}, false},
		{"admin lifts a lower role to the very top", adminAtB, []uuid.UUID{d, a, b, c}, false},
		{"without a rank nothing can move", adminNoRoles, []uuid.UUID{a, b, d, c}, false},
	}
	for _, tc := range cases {
		if got := role.CanReorder(tc.actor, current, tc.newOrder); got != tc.want {
			t.Errorf("%s: CanReorder = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestCanReorder_NoRoles(t *testing.T) {
	admin := role.Standing{IsMember: true, Permissions: role.PermAdministrator, TopPosition: pos(1)}
	if !role.CanReorder(admin, nil, nil) {
		t.Error("an empty hierarchy should be reorderable (to itself)")
	}
}
