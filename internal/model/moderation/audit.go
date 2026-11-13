package moderation

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model"
)

const (
	AuditActionBan    = "ban"
	AuditActionUnban  = "unban"
	AuditActionKick   = "kick"
	AuditActionMute   = "mute"
	AuditActionUnmute = "unmute"
)

type AuditEntry struct {
	ID           uuid.UUID `json:"id"`
	ServerID     uuid.UUID `json:"server_id"`
	Action       string    `json:"action"`
	ActorUserID  string    `json:"actor_user_id"`
	TargetUserID string    `json:"target_user_id"`
	OccurredAt   time.Time `json:"occurred_at"`
	// Replicated from identify; nil until it arrives. The target of a kick or
	// ban is no longer a member, so this is the only way to name them.
	ActorProfile  *model.MemberProfileResponse `json:"actor_profile"`
	TargetProfile *model.MemberProfileResponse `json:"target_profile"`
}

type AuditPage struct {
	Entries []AuditEntry `json:"entries"`
	Total   int          `json:"total"`
	Limit   int          `json:"limit"`
	Offset  int          `json:"offset"`
}

type auditContextKey struct{}

func WithAudit(ctx context.Context, entry AuditEntry) context.Context {
	return context.WithValue(ctx, auditContextKey{}, entry)
}

func AuditFromContext(ctx context.Context) (AuditEntry, bool) {
	entry, ok := ctx.Value(auditContextKey{}).(AuditEntry)
	return entry, ok
}
