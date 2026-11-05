package invitation

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/events-contract/envelope"
	"github.com/discordia-grupo01/events-contract/membership"
	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/model/role"
	"github.com/discordia-grupo01/servers/internal/outbox"
	"github.com/discordia-grupo01/servers/internal/service"
)

const (
	codeAttempts = 5
	// TTL es cuánto vive un link de invitación desde que se genera. Fijo:
	// el cliente nunca lo manda.
	TTL = 7 * 24 * time.Hour
)

type Service struct {
	repo      Repository
	servers   service.ServerGetter
	standings service.StandingReader
	now       func() time.Time
}

func NewService(repo Repository, servers service.ServerGetter, standings service.StandingReader) *Service {
	return &Service{
		repo:      repo,
		servers:   servers,
		standings: standings,
		now:       func() time.Time { return time.Now().UTC() },
	}
}

// standing loads userID's standing in serverID, reporting an unknown server
// with this package's own not-found error.
func (s *Service) standing(ctx context.Context, serverID uuid.UUID, userID string) (role.Standing, error) {
	srv, err := s.servers.GetByID(ctx, serverID)
	if errors.Is(err, model.ErrServerNotFound) {
		return role.Standing{}, model.ErrInviteServerNotFound
	}
	if err != nil {
		return role.Standing{}, err
	}
	return service.ResolveStanding(ctx, s.standings, srv, userID)
}

func (s *Service) GenerateInvitation(ctx context.Context, p GenerateParams) (model.Invitation, error) {
	if p.CreatedBy == "" {
		return model.Invitation{}, model.ErrUnauthorized
	}

	serverID, err := uuid.Parse(p.ServerID)
	if err != nil {
		return model.Invitation{}, model.ErrInviteServerNotFound
	}

	actor, err := s.standing(ctx, serverID, p.CreatedBy)
	if err != nil {
		return model.Invitation{}, err
	}
	if err := service.RequireCreateInvite(actor); err != nil {
		return model.Invitation{}, err
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

	actor, err := s.standing(ctx, serverID, p.UserID)
	if err != nil {
		return nil, err
	}
	if err := service.RequireCreateInvite(actor); err != nil {
		return nil, err
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

	actor, err := s.standing(ctx, inv.ServerID, p.UserID)
	if err != nil {
		return err
	}
	if err := service.RequireRevokeInvite(actor, p.UserID, inv); err != nil {
		return err
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
	if errors.Is(err, model.ErrInvitationNotFound) {
		return s.joinByVanity(ctx, p)
	}
	if err != nil {
		return JoinResult{}, err
	}

	if done, result, err := s.checkCanJoin(ctx, inv.ServerID, p.UserID); done || err != nil {
		return result, err
	}

	if err := inv.Validate(s.now()); err != nil {
		return JoinResult{}, err
	}

	occurredAt := s.now()
	serverID, err := s.repo.RedeemInvitation(ctx, inv.Code, p.UserID, occurredAt, memberJoinedEvent(p.UserID, occurredAt))
	switch {
	case err == nil:
		return JoinResult{ServerID: serverID, AlreadyMember: false}, nil
	case errors.Is(err, model.ErrAlreadyMember):
		return JoinResult{ServerID: inv.ServerID, AlreadyMember: true}, nil
	default:
		return JoinResult{}, err
	}
}

func (s *Service) joinByVanity(ctx context.Context, p JoinParams) (JoinResult, error) {
	serverID, err := s.repo.ServerByVanity(ctx, strings.ToLower(p.Code))
	if err != nil {
		return JoinResult{}, err
	}
	if done, result, err := s.checkCanJoin(ctx, serverID, p.UserID); done || err != nil {
		return result, err
	}
	occurredAt := s.now()
	err = s.repo.JoinByVanity(ctx, serverID, p.UserID, occurredAt, memberJoinedEvent(p.UserID, occurredAt))
	switch {
	case err == nil:
		return JoinResult{ServerID: serverID}, nil
	case errors.Is(err, model.ErrAlreadyMember):
		return JoinResult{ServerID: serverID, AlreadyMember: true}, nil
	default:
		return JoinResult{}, err
	}
}

// checkCanJoin rejects a banned user, and is done without an error if they
// already are a member.
func (s *Service) checkCanJoin(ctx context.Context, serverID uuid.UUID, userID string) (bool, JoinResult, error) {
	banned, err := s.repo.IsBanned(ctx, serverID, userID)
	if err != nil {
		return false, JoinResult{}, err
	}
	if banned {
		return false, JoinResult{}, model.ErrUserBanned
	}
	member, err := s.repo.IsMember(ctx, serverID, userID)
	if err != nil {
		return false, JoinResult{}, err
	}
	if member {
		return true, JoinResult{ServerID: serverID, AlreadyMember: true}, nil
	}
	return false, JoinResult{}, nil
}

func memberJoinedEvent(userID string, occurredAt time.Time) func(uuid.UUID) (outbox.Event, error) {
	return func(serverID uuid.UUID) (outbox.Event, error) {
		return outbox.New(membership.RoutingKeyMemberJoined, membership.MemberJoined{
			Meta:     envelope.Meta{EventID: uuid.NewString(), OccurredAt: occurredAt},
			ServerID: serverID.String(),
			UserID:   userID,
		})
	}
}

func newCode() (string, error) {
	var b [9]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}
