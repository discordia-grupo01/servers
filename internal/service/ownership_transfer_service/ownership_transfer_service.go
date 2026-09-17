package ownership_transfer_service

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model"
)

type Service struct {
	servers   Servers
	members   Members
	transfers Transfers
	now       func() time.Time
}

func New(servers Servers, members Members, transfers Transfers) *Service {
	return &Service{
		servers: servers, members: members, transfers: transfers,
		now: func() time.Time { return time.Now().UTC() },
	}
}

func (s *Service) InitiateTransfer(ctx context.Context, p InitiateParams) (model.OwnershipTransfer, error) {
	if p.ActorUserID == "" {
		return model.OwnershipTransfer{}, model.ErrUnauthorized
	}
	if p.ToUserID == "" {
		return model.OwnershipTransfer{}, model.ErrTransferTargetRequired
	}

	srv, err := s.servers.GetByID(ctx, p.ServerID)
	if err != nil {
		return model.OwnershipTransfer{}, err
	}
	if srv.OwnerID != p.ActorUserID {
		return model.OwnershipTransfer{}, model.ErrForbiddenNotOwner
	}
	if p.ToUserID == srv.OwnerID {
		return model.OwnershipTransfer{}, model.ErrTransferTargetIsOwner
	}

	isMember, err := s.members.IsMember(ctx, p.ServerID, p.ToUserID)
	if err != nil {
		return model.OwnershipTransfer{}, err
	}
	if !isMember {
		return model.OwnershipTransfer{}, model.ErrTransferTargetNotMember
	}

	if existing, err := s.transfers.GetPendingByServer(ctx, p.ServerID); err == nil {
		s.expireIfStale(ctx, existing)
	}

	t := model.OwnershipTransfer{
		ID:         uuid.New(),
		ServerID:   p.ServerID,
		FromUserID: p.ActorUserID,
		ToUserID:   p.ToUserID,
		Status:     model.OwnershipTransferPending,
		CreatedAt:  s.now(),
	}
	if err := s.transfers.Create(ctx, t); err != nil {
		return model.OwnershipTransfer{}, err
	}
	return t, nil
}

// GetPendingTransfer returns a server's pending ownership transfer, if any.
// Only members of the server (the owner initiating it, or the member it was
// sent to) can see it.
func (s *Service) GetPendingTransfer(ctx context.Context, p GetPendingParams) (model.OwnershipTransfer, error) {
	if p.ActorUserID == "" {
		return model.OwnershipTransfer{}, model.ErrUnauthorized
	}

	if _, err := s.servers.GetByID(ctx, p.ServerID); err != nil {
		return model.OwnershipTransfer{}, err
	}

	isMember, err := s.members.IsMember(ctx, p.ServerID, p.ActorUserID)
	if err != nil {
		return model.OwnershipTransfer{}, err
	}
	if !isMember {
		return model.OwnershipTransfer{}, model.ErrForbiddenNotMember
	}

	t, err := s.transfers.GetPendingByServer(ctx, p.ServerID)
	if err != nil {
		return model.OwnershipTransfer{}, err
	}
	if _, expired := s.expireIfStale(ctx, t); expired {
		return model.OwnershipTransfer{}, model.ErrOwnershipTransferNotFound
	}
	return t, nil
}

func (s *Service) AcceptTransfer(ctx context.Context, p RespondParams) (model.OwnershipTransfer, error) {
	if _, err := s.resolveRespondable(ctx, p); err != nil {
		return model.OwnershipTransfer{}, err
	}
	t, err := s.transfers.Accept(ctx, p.TransferID, s.now())
	if err != nil {
		return model.OwnershipTransfer{}, err
	}
	return t, nil
}

func (s *Service) RejectTransfer(ctx context.Context, p RespondParams) (model.OwnershipTransfer, error) {
	if _, err := s.resolveRespondable(ctx, p); err != nil {
		return model.OwnershipTransfer{}, err
	}
	t, err := s.transfers.Reject(ctx, p.TransferID, s.now())
	if err != nil {
		return model.OwnershipTransfer{}, err
	}
	return t, nil
}

func (s *Service) CancelTransfer(ctx context.Context, p RespondParams) (model.OwnershipTransfer, error) {
	if p.ActorUserID == "" {
		return model.OwnershipTransfer{}, model.ErrUnauthorized
	}
	t, err := s.transfers.GetByID(ctx, p.TransferID)
	if err != nil {
		return model.OwnershipTransfer{}, err
	}
	if t.ServerID != p.ServerID {
		return model.OwnershipTransfer{}, model.ErrOwnershipTransferServerMismatch
	}
	if t.FromUserID != p.ActorUserID {
		return model.OwnershipTransfer{}, model.ErrOwnershipTransferNotInitiator
	}
	if !t.IsPending() {
		return model.OwnershipTransfer{}, model.ErrOwnershipTransferNotPending
	}

	return s.transfers.Cancel(ctx, p.TransferID, s.now())
}

func (s *Service) resolveRespondable(ctx context.Context, p RespondParams) (model.OwnershipTransfer, error) {
	if p.ActorUserID == "" {
		return model.OwnershipTransfer{}, model.ErrUnauthorized
	}
	t, err := s.transfers.GetByID(ctx, p.TransferID)
	if err != nil {
		return model.OwnershipTransfer{}, err
	}
	if t.ServerID != p.ServerID {
		return model.OwnershipTransfer{}, model.ErrOwnershipTransferServerMismatch
	}
	if t.ToUserID != p.ActorUserID {
		return model.OwnershipTransfer{}, model.ErrOwnershipTransferWrongTarget
	}
	if !t.IsPending() {
		return model.OwnershipTransfer{}, model.ErrOwnershipTransferNotPending
	}
	if _, expired := s.expireIfStale(ctx, t); expired {
		return model.OwnershipTransfer{}, model.ErrOwnershipTransferNotPending
	}
	return t, nil
}

func (s *Service) expireIfStale(ctx context.Context, t model.OwnershipTransfer) (model.OwnershipTransfer, bool) {
	if !t.IsPending() || s.now().Sub(t.CreatedAt) <= model.OwnershipTransferTTL {
		return t, false
	}
	rejected, err := s.transfers.Reject(ctx, t.ID, s.now())
	if err != nil {
		return t, false
	}
	return rejected, true
}
