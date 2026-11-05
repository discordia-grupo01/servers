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
	// PermCreateInvite lets its holder generate and list the server's
	// invitations (administrators hold it implicitly).
	PermCreateInvite
	// PermManageMessages lets its holder delete other members' messages
	// (administrators hold it implicitly).
	PermManageMessages
	// PermMentionEveryone lets its holder mention @everyone and any role in a
	// message, notifying every member reached (administrators hold it
	// implicitly). Without it those mentions stay plain text.
	PermMentionEveryone
	// PermAddReactions lets its holder react to messages with emojis (#48).
	PermAddReactions
	PermConnect
)

// DefaultPermissions are the permissions a newly created role starts with
// (#21 CA1): enough to see the server's channels, talk and react in them and
// join its voice channels. Roles created before this default existed keep
// whatever they had.
const DefaultPermissions = PermViewChannels | PermSendMessages | PermAddReactions | PermConnect

// ChannelScopedPermissions are the permissions that mean something in a
// single channel, so a role's value for them can be overridden per channel
// (#57). The rest act on the whole server. Its value (6659) is repeated in the
// overrides_channel_scoped CHECK of migration 0025: keep them in sync.
const ChannelScopedPermissions = PermViewChannels | PermSendMessages | PermManageMessages | PermAddReactions | PermConnect

const ModeratorPermissions = PermManageMessages | PermKickMembers | PermBanMembers

// AllPermissions is every permission in the catalog. PermConnect is the last
// bit, so all bits up to and including it are set.
const AllPermissions = PermConnect<<1 - 1

var permissionsByName = map[string]Permission{
	"VIEW_CHANNELS":    PermViewChannels,
	"SEND_MESSAGES":    PermSendMessages,
	"MANAGE_CHANNELS":  PermManageChannels,
	"MANAGE_ROLES":     PermManageRoles,
	"KICK_MEMBERS":     PermKickMembers,
	"BAN_MEMBERS":      PermBanMembers,
	"MANAGE_SERVER":    PermManageServer,
	"ADMINISTRATOR":    PermAdministrator,
	"CREATE_INVITE":    PermCreateInvite,
	"MANAGE_MESSAGES":  PermManageMessages,
	"MENTION_EVERYONE": PermMentionEveryone,
	"ADD_REACTIONS":    PermAddReactions,
	"CONNECT":          PermConnect,
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
