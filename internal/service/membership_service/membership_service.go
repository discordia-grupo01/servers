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

// ListMembers requires the caller to already be a member of the server --
// it exists to let an owner pick an ownership-transfer target, not to
// expose membership to outsiders.
func (s *MembershipService) ListMembers(ctx context.Context, p ListMembersParams) ([]model.Member, int, error) {
	if p.ActorUserID == "" {
		return nil, 0, model.ErrUnauthorized
	}

	if _, err := s.servers.GetByID(ctx, p.ServerID); err != nil {
		return nil, 0, err
	}

	isMember, err := s.members.IsMember(ctx, p.ServerID, p.ActorUserID)
	if err != nil {
		return nil, 0, err
	}
	if !isMember {
		return nil, 0, model.ErrForbiddenNotMember
	}

	limit := p.Limit
	switch {
	case limit <= 0:
		limit = model.MemberListDefaultLimit
	case limit > model.MemberListMaxLimit:
		limit = model.MemberListMaxLimit
	}
	offset := p.Offset
	if offset < 0 {
		offset = 0
	}

	return s.members.ListMembers(ctx, p.ServerID, limit, offset)
}
