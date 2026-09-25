package service_test

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/outbox"
	"github.com/discordia-grupo01/servers/internal/service/invitation"
)

const (
	inviterID = "user-inviter"
	joinerID  = "user-joiner"
)

type fakeInviteRepo struct {
	exists    bool
	existsErr error

	member    bool
	memberErr error

	banned    bool
	bannedErr error

	collisions  int
	createErr   error
	created     model.Invitation
	createCalls int

	byCode    map[string]model.Invitation
	byCodeErr error

	listResult []model.Invitation
	listErr    error
	listCalls  int

	revokeErr   error
	revokedCode string
	revokedAt   time.Time
	revokeCalls int

	redeemServer   uuid.UUID
	redeemErr      error
	redeemCalls    int
	gotOutboxEvent outbox.Event
}

func (f *fakeInviteRepo) ServerExists(context.Context, uuid.UUID) (bool, error) {
	return f.exists, f.existsErr
}

func (f *fakeInviteRepo) IsMember(context.Context, uuid.UUID, string) (bool, error) {
	return f.member, f.memberErr
}

func (f *fakeInviteRepo) IsBanned(context.Context, uuid.UUID, string) (bool, error) {
	return f.banned, f.bannedErr
}

func (f *fakeInviteRepo) CreateInvitation(_ context.Context, inv model.Invitation) error {
	f.createCalls++
	if f.createErr != nil {
		return f.createErr
	}
	if f.collisions > 0 {
		f.collisions--
		return model.ErrInviteCodeUnavailable
	}
	f.created = inv
	return nil
}

func (f *fakeInviteRepo) InvitationByCode(_ context.Context, code string) (model.Invitation, error) {
	if f.byCodeErr != nil {
		return model.Invitation{}, f.byCodeErr
	}
	if inv, ok := f.byCode[code]; ok {
		return inv, nil
	}
	return model.Invitation{}, model.ErrInvitationNotFound
}

func (f *fakeInviteRepo) ListByServer(context.Context, uuid.UUID) ([]model.Invitation, error) {
	f.listCalls++
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.listResult, nil
}

func (f *fakeInviteRepo) RevokeInvitation(_ context.Context, code string, at time.Time) error {
	f.revokeCalls++
	if f.revokeErr != nil {
		return f.revokeErr
	}
	f.revokedCode, f.revokedAt = code, at
	return nil
}

func (f *fakeInviteRepo) RedeemInvitation(_ context.Context, _, _ string, _ time.Time, buildEvent func(uuid.UUID) (outbox.Event, error)) (uuid.UUID, error) {
	f.redeemCalls++
	if f.redeemErr != nil {
		return uuid.Nil, f.redeemErr
	}
	evt, err := buildEvent(f.redeemServer)
	if err != nil {
		return uuid.Nil, err
	}
	f.gotOutboxEvent = evt
	return f.redeemServer, nil
}

func newInviteService(repo *fakeInviteRepo) *invitation.Service {
	return invitation.NewService(repo)
}

func activeInvitation(serverID uuid.UUID) model.Invitation {
	return model.Invitation{
		ID:        uuid.New(),
		Code:      "active-code",
		ServerID:  serverID,
		CreatedBy: inviterID,
		CreatedAt: time.Now().UTC().Add(-time.Hour),
	}
}

func ptrInt(n int) *int { return &n }

func past() *time.Time {
	t := time.Now().UTC().Add(-time.Minute)
	return &t
}
