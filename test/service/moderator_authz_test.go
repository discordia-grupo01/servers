package service_test

import (
	"errors"
	"testing"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/model/role"
	"github.com/discordia-grupo01/servers/internal/service"
)

func TestRequireModerator(t *testing.T) {
	cases := map[string]struct {
		st      role.Standing
		allowed bool
	}{
		"owner":                          {role.Standing{IsMember: true, IsOwner: true}, true},
		"administrator":                  {role.Standing{IsMember: true, Permissions: role.PermAdministrator}, true},
		"the three moderation grants":    {role.Standing{IsMember: true, Permissions: role.PermManageMessages | role.PermKickMembers | role.PermBanMembers}, true},
		"the three plus others":          {role.Standing{IsMember: true, Permissions: role.DefaultPermissions | role.ModeratorPermissions}, true},
		"only MANAGE_MESSAGES":           {role.Standing{IsMember: true, Permissions: role.PermManageMessages}, false},
		"only KICK_MEMBERS":              {role.Standing{IsMember: true, Permissions: role.PermKickMembers}, false},
		"only BAN_MEMBERS":               {role.Standing{IsMember: true, Permissions: role.PermBanMembers}, false},
		"MANAGE_MESSAGES and KICK":       {role.Standing{IsMember: true, Permissions: role.PermManageMessages | role.PermKickMembers}, false},
		"KICK and BAN without messages":  {role.Standing{IsMember: true, Permissions: role.PermKickMembers | role.PermBanMembers}, false},
		"MANAGE_MESSAGES and BAN":        {role.Standing{IsMember: true, Permissions: role.PermManageMessages | role.PermBanMembers}, false},
		"member without roles":           {role.Standing{IsMember: true}, false},
		"non member":                     {role.Standing{}, false},
		"non member with the three bits": {role.Standing{Permissions: role.ModeratorPermissions}, false},
	}
	for name, tc := range cases {
		err := service.RequireModerator(tc.st)
		if tc.allowed && err != nil {
			t.Errorf("%s: unexpected error %v", name, err)
		}
		if !tc.allowed && !errors.Is(err, model.ErrForbiddenMissingPermission) {
			t.Errorf("%s: err = %v, want ErrForbiddenMissingPermission", name, err)
		}
	}
}

func TestModeratorPermissions_AreExactlyTheThreeModerationGrants(t *testing.T) {
	want := role.PermManageMessages | role.PermKickMembers | role.PermBanMembers
	if role.ModeratorPermissions != want {
		t.Fatalf("ModeratorPermissions = %v, want %v", role.ModeratorPermissions, want)
	}
}
