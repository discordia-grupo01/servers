package membership_service

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/events-contract/envelope"
	"github.com/discordia-grupo01/events-contract/membership"
	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/outbox"
)

type MembershipService struct {
	servers Servers
	members Members
	now     func() time.Time
}

func New(servers Servers, members Members) *MembershipService {
	return &MembershipService{
		servers: servers,
		members: members,
		now:     func() time.Time { return time.Now().UTC() },
	}
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

	memberLeftEvent, err := outbox.New(membership.RoutingKeyMemberLeft, membership.MemberLeft{
		Meta:     envelope.Meta{EventID: uuid.NewString(), OccurredAt: s.now()},
		ServerID: p.ServerID.String(),
		UserID:   p.ActorUserID,
	})
	if err != nil {
		return err
	}

	removed, err := s.members.RemoveMember(ctx, p.ServerID, p.ActorUserID, memberLeftEvent)
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

	limit, offset := model.ClampMemberPage(p.Limit, p.Offset)
	return s.members.ListMembers(ctx, p.ServerID, p.Search, limit, offset)
}
