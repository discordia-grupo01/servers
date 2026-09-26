package role

import "sort"

// Permission is a single bit in a Role's permission bitmask.
type Permission = int64

// Closed catalog of permissions a role can carry. The HTTP API never exposes
// this bitmask directly: clients send/receive permission names (see
// ParsePermissions / PermissionNames), and the service layer translates.
const (
	PermViewChannels Permission = 1 << iota
	PermSendMessages
	PermManageChannels
	PermManageRoles
	PermKickMembers
	PermBanMembers
	PermManageServer
	// PermAdministrator grants every other permission and makes its holder a
	// server administrator (see Standing.IsAdmin).
	PermAdministrator
)

var permissionsByName = map[string]Permission{
	"VIEW_CHANNELS":   PermViewChannels,
	"SEND_MESSAGES":   PermSendMessages,
	"MANAGE_CHANNELS": PermManageChannels,
	"MANAGE_ROLES":    PermManageRoles,
	"KICK_MEMBERS":    PermKickMembers,
	"BAN_MEMBERS":     PermBanMembers,
	"MANAGE_SERVER":   PermManageServer,
	"ADMINISTRATOR":   PermAdministrator,
}

// ParsePermissions translates permission names into a bitmask. If any name is
// not in the catalog, it returns ok=false and the offending name so the
// caller can build a precise error.
func ParsePermissions(names []string) (mask Permission, invalidName string, ok bool) {
	for _, name := range names {
		p, known := permissionsByName[name]
		if !known {
			return 0, name, false
		}
		mask |= p
	}
	return mask, "", true
}

// PermissionNames translates a bitmask back into its permission names, sorted
// for deterministic output.
func PermissionNames(mask Permission) []string {
	names := make([]string, 0, len(permissionsByName))
	for name, p := range permissionsByName {
		if mask&p != 0 {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}
