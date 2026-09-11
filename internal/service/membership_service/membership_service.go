package membership_service

import (
	"context"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model"
)

type Servers interface {
	GetByID(ctx context.Context, id uuid.UUID) (model.Server, error)
}

type Members interface {
	RemoveMember(ctx context.Context, serverID uuid.UUID, userID string) (bool, error)
}

type MembershipService struct {
	servers Servers
	members Members
}

func New(servers Servers, members Members) *MembershipService {
	return &MembershipService{servers: servers, members: members}
}

type LeaveParams struct {
	ServerID     uuid.UUID
	ActorUserID  string
	TargetUserID string
}

func (s *MembershipService) LeaveServer(ctx context.Context, p LeaveParams) error {
	if p.ActorUserID == "" {
		return model.ErrUnauthorized
	}
	if p.TargetUserID != p.ActorUserID {
		return model.ErrLeaveForbidden
	}

	srv, err := s.servers.GetByID(ctx, p.ServerID)
	if err != nil {
		return err
	}
	if srv.OwnerID == p.ActorUserID {
		return model.ErrOwnerCannotLeave
	}

	removed, err := s.members.RemoveMember(ctx, p.ServerID, p.ActorUserID)
	if err != nil {
		return err
	}
	if !removed {
		return model.ErrMemberNotFound
	}
	return nil
}
