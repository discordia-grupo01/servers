package service

import (
	"context"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model"
)

type ServerGetter interface {
	GetByID(ctx context.Context, id uuid.UUID) (model.Server, error)
}

type MemberChecker interface {
	IsMember(ctx context.Context, serverID uuid.UUID, userID string) (bool, error)
}
