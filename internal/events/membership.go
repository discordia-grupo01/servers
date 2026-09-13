package events

import "time"

const (
	RoutingKeyMemberJoined = "servers.member_joined"
	RoutingKeyMemberLeft   = "servers.member_left"
)

type MembershipEvent struct {
	EventID    string    `json:"event_id"`
	ServerID   string    `json:"server_id"`
	UserID     string    `json:"user_id"`
	OccurredAt time.Time `json:"occurred_at"`
}
