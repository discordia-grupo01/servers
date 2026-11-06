package moderation

import (
	"context"
	"time"

	"github.com/google/uuid"
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
