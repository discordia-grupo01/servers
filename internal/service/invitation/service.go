package invitation

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/events-contract/envelope"
	"github.com/discordia-grupo01/events-contract/membership"
	"github.com/discordia-grupo01/servers/internal/events"
	"github.com/discordia-grupo01/servers/internal/model"
)

const (
	codeAttempts = 5
	// TTL es cuánto vive un link de invitación desde que se genera. Fijo:
	// el cliente nunca lo manda.
	TTL = 7 * 24 * time.Hour
)

type Service struct {
	repo   Repository
	events events.Publisher
	now    func() time.Time
}

func NewService(repo Repository, publisher events.Publisher) *Service {
	return &Service{repo: repo, events: publisher, now: func() time.Time { return time.Now().UTC() }}
}

func (s *Service) GenerateInvitation(ctx context.Context, p GenerateParams) (model.Invitation, error) {
	if p.CreatedBy == "" {
		return model.Invitation{}, model.ErrUnauthorized
	}

	serverID, err := uuid.Parse(p.ServerID)
	if err != nil {
		return model.Invitation{}, model.ErrInviteServerNotFound
	}

	exists, err := s.repo.ServerExists(ctx, serverID)
	if err != nil {
		return model.Invitation{}, err
	}
	if !exists {
		return model.Invitation{}, model.ErrInviteServerNotFound
	}

	member, err := s.repo.IsMember(ctx, serverID, p.CreatedBy)
	if err != nil {
		return model.Invitation{}, err
	}
	if !member {
		return model.Invitation{}, model.ErrInvitePermissionDenied
	}

	if p.MaxUses != nil && *p.MaxUses <= 0 {
		return model.Invitation{}, model.ErrInviteMaxUsesInvalid
	}

	now := s.now()
	expiresAt := now.Add(TTL)

	inv := model.Invitation{
		ServerID:  serverID,
		CreatedBy: p.CreatedBy,
		MaxUses:   p.MaxUses,
		ExpiresAt: &expiresAt,
		CreatedAt: now,
	}

	for attempt := 0; attempt < codeAttempts; attempt++ {
		code, err := newCode()
		if err != nil {
			return model.Invitation{}, err
		}
		inv.ID = uuid.New()
		inv.Code = code

		switch err := s.repo.CreateInvitation(ctx, inv); {
		case err == nil:
			return inv, nil
		case errors.Is(err, model.ErrInviteCodeUnavailable):
			continue
		default:
			return model.Invitation{}, err
		}
	}
	return model.Invitation{}, model.ErrInviteCodeUnavailable
}

func (s *Service) ListInvitations(ctx context.Context, p ListParams) ([]model.Invitation, error) {
	if p.UserID == "" {
		return nil, model.ErrUnauthorized
	}

	serverID, err := uuid.Parse(p.ServerID)
	if err != nil {
		return nil, model.ErrInviteServerNotFound
	}

	exists, err := s.repo.ServerExists(ctx, serverID)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, model.ErrInviteServerNotFound
	}

	member, err := s.repo.IsMember(ctx, serverID, p.UserID)
	if err != nil {
		return nil, err
	}
	if !member {
		return nil, model.ErrInvitePermissionDenied
	}

	return s.repo.ListByServer(ctx, serverID)
}

func (s *Service) RevokeInvitation(ctx context.Context, p RevokeParams) error {
	if p.UserID == "" {
		return model.ErrUnauthorized
	}

	inv, err := s.repo.InvitationByCode(ctx, p.Code)
	if err != nil {
		return err
	}

	member, err := s.repo.IsMember(ctx, inv.ServerID, p.UserID)
	if err != nil {
		return err
	}
	if !member {
		return model.ErrInvitePermissionDenied
	}

	if inv.IsRevoked() {
		return nil
	}
	return s.repo.RevokeInvitation(ctx, inv.Code, s.now())
}

func (s *Service) JoinServer(ctx context.Context, p JoinParams) (JoinResult, error) {
	if p.UserID == "" {
		return JoinResult{}, model.ErrUnauthorized
	}

	inv, err := s.repo.InvitationByCode(ctx, p.Code)
	if err != nil {
		return JoinResult{}, err
	}

	banned, err := s.repo.IsBanned(ctx, inv.ServerID, p.UserID)
	if err != nil {
		return JoinResult{}, err
	}
	if banned {
		return JoinResult{}, model.ErrUserBanned
	}

	member, err := s.repo.IsMember(ctx, inv.ServerID, p.UserID)
	if err != nil {
		return JoinResult{}, err
	}
	if member {
		return JoinResult{ServerID: inv.ServerID, AlreadyMember: true}, nil
	}

	if err := inv.Validate(s.now()); err != nil {
		return JoinResult{}, err
	}

	serverID, err := s.repo.RedeemInvitation(ctx, inv.Code, p.UserID, s.now())
	switch {
	case err == nil:
		s.publishMemberJoined(ctx, serverID, p.UserID)
		return JoinResult{ServerID: serverID, AlreadyMember: false}, nil
	case errors.Is(err, model.ErrAlreadyMember):
		return JoinResult{ServerID: inv.ServerID, AlreadyMember: true}, nil
	default:
		return JoinResult{}, err
	}
}

func (s *Service) publishMemberJoined(ctx context.Context, serverID uuid.UUID, userID string) {
	_ = s.events.Publish(ctx, membership.RoutingKeyMemberJoined, membership.MemberJoined{
		Meta:     envelope.Meta{EventID: uuid.NewString(), OccurredAt: s.now()},
		ServerID: serverID.String(),
		UserID:   userID,
	})
}

func newCode() (string, error) {
	var b [9]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}
