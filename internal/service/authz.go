package service

import "github.com/discordia-grupo01/servers/internal/model"

// RequireManageRoles checks whether userID is allowed to manage roles (and
// role assignments) in srv. Today this is a simple owner check, but it is
// named after the capability it represents (not "require owner") so that
// when a granular MANAGE_ROLES permission exists, only this function needs
// to change -- not every call site.
//
// Exported (not requireManageRoles) because role_service and
// member_role_service live in their own subpackages and need to call it from
// outside package service.
func RequireManageRoles(srv model.Server, userID string) error {
	if srv.OwnerID != userID {
		return model.ErrForbiddenNotOwner
	}
	return nil
}

// RequireManageChannels checks whether userID is allowed to manage channels
// and categories in srv. Same "owner only" scope as RequireManageRoles today
// (see its doc comment) even though MANAGE_CHANNELS already exists in the
// permission catalog -- no RequireManageX function uses the bitmask yet.
func RequireManageChannels(srv model.Server, userID string) error {
	if srv.OwnerID != userID {
		return model.ErrForbiddenNotOwner
	}
	return nil
}

func RequireManageServer(srv model.Server, userID string) error {
	if srv.OwnerID != userID {
		return model.ErrForbiddenNotOwner
	}
	return nil
}
