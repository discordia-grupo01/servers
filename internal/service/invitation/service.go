package invitation

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model"
)

const codeAttempts = 5

type Service struct {
	repo Repository
	now  func() time.Time
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo, now: func() time.Time { return time.Now().UTC() }}
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

	now := s.now()
	if p.MaxUses != nil && *p.MaxUses <= 0 {
		return model.Invitation{}, model.ErrInviteMaxUsesInvalid
	}
	if p.ExpiresAt != nil && !p.ExpiresAt.After(now) {
		return model.Invitation{}, model.ErrInviteExpirationInvalid
	}

	inv := model.Invitation{
		ServerID:  serverID,
		CreatedBy: p.CreatedBy,
		MaxUses:   p.MaxUses,
		ExpiresAt: toUTC(p.ExpiresAt),
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
		return JoinResult{ServerID: serverID, AlreadyMember: false}, nil
	case errors.Is(err, model.ErrAlreadyMember):
		return JoinResult{ServerID: inv.ServerID, AlreadyMember: true}, nil
	default:
		return JoinResult{}, err
	}
}

func newCode() (string, error) {
	var b [9]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}

func toUTC(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}
