package membership_service

import (
	"context"

	"github.com/discordia-grupo01/servers/internal/model"
)

type MembershipService struct {
	servers Servers
	members Members
}

func New(servers Servers, members Members) *MembershipService {
	return &MembershipService{servers: servers, members: members}
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
