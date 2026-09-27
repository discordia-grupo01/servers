package service_test

import (
	"errors"
	"testing"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/model/role"
	"github.com/discordia-grupo01/servers/internal/service"
)

func TestRequirePermission(t *testing.T) {
	cases := map[string]struct {
		st      role.Standing
		allowed bool
	}{
		"owner":                           {role.Standing{IsMember: true, IsOwner: true}, true},
		"administrator":                   {role.Standing{IsMember: true, Permissions: role.PermAdministrator}, true},
		"member with BAN_MEMBERS":         {role.Standing{IsMember: true, Permissions: role.PermBanMembers}, true},
		"member with other permissions":   {role.Standing{IsMember: true, Permissions: role.PermKickMembers | role.PermManageRoles}, false},
		"member without roles":            {role.Standing{IsMember: true}, false},
		"non member":                      {role.Standing{}, false},
		"non member with stale perm bits": {role.Standing{Permissions: role.PermBanMembers}, false},
	}
	for name, tc := range cases {
		err := service.RequirePermission(tc.st, role.PermBanMembers)
		if tc.allowed && err != nil {
			t.Errorf("%s: unexpected error %v", name, err)
		}
		if !tc.allowed && !errors.Is(err, model.ErrForbiddenMissingPermission) {
			t.Errorf("%s: err = %v, want ErrForbiddenMissingPermission", name, err)
		}
	}
}

func TestRequireOutranks(t *testing.T) {
	one, two := 1, 2
	high := role.Standing{IsMember: true, TopPosition: &one}
	low := role.Standing{IsMember: true, TopPosition: &two}
	if err := service.RequireOutranks(high, low); err != nil {
		t.Errorf("higher over lower: unexpected error %v", err)
	}
	for name, pair := range map[string][2]role.Standing{
		"equal rank": {low, low},
		"lower rank": {low, high},
		"the owner":  {high, {IsMember: true, IsOwner: true}},
	} {
		if err := service.RequireOutranks(pair[0], pair[1]); !errors.Is(err, model.ErrTargetOutranksActor) {
			t.Errorf("%s: err = %v, want ErrTargetOutranksActor", name, err)
		}
	}
}
