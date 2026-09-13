package ownership_transfer_service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/service/ownership_transfer_service"
)

func validInitiateParams(srv model.Server) ownership_transfer_service.InitiateParams {
	return ownership_transfer_service.InitiateParams{
		ServerID: srv.ID, ActorUserID: testOwner, ToUserID: testMember,
	}
}

func TestInitiateTransfer_Success(t *testing.T) {
	srv := ownerServer()
	transfers := &fakeTransfers{}
	svc := newService(&fakeServers{server: srv}, &fakeMembers{isMember: true}, transfers)

	got, err := svc.InitiateTransfer(context.Background(), validInitiateParams(srv))
	if err != nil {
		t.Fatalf("InitiateTransfer() error = %v, want nil", err)
	}
	if transfers.createdN != 1 {
		t.Fatalf("Create calls = %d, want 1", transfers.createdN)
	}
	if got.Status != model.OwnershipTransferPending {
		t.Errorf("status = %q, want pending", got.Status)
	}
	if got.FromUserID != testOwner || got.ToUserID != testMember {
		t.Errorf("from/to = %q/%q, want %q/%q", got.FromUserID, got.ToUserID, testOwner, testMember)
	}
}

func TestInitiateTransfer_Unauthenticated(t *testing.T) {
	svc := newService(&fakeServers{server: ownerServer()}, &fakeMembers{isMember: true}, &fakeTransfers{})

	p := ownership_transfer_service.InitiateParams{ServerID: uuid.New(), ToUserID: testMember}
	if _, err := svc.InitiateTransfer(context.Background(), p); !errors.Is(err, model.ErrUnauthorized) {
		t.Errorf("err = %v, want ErrUnauthorized", err)
	}
}

func TestInitiateTransfer_TargetRequired(t *testing.T) {
	srv := ownerServer()
	svc := newService(&fakeServers{server: srv}, &fakeMembers{isMember: true}, &fakeTransfers{})

	p := validInitiateParams(srv)
	p.ToUserID = ""
	if _, err := svc.InitiateTransfer(context.Background(), p); !errors.Is(err, model.ErrTransferTargetRequired) {
		t.Errorf("err = %v, want ErrTransferTargetRequired", err)
	}
}

func TestInitiateTransfer_NotOwner_Forbidden(t *testing.T) {
	srv := ownerServer()
	transfers := &fakeTransfers{}
	svc := newService(&fakeServers{server: srv}, &fakeMembers{isMember: true}, transfers)

	p := validInitiateParams(srv)
	p.ActorUserID = "someone-else"

	if _, err := svc.InitiateTransfer(context.Background(), p); !errors.Is(err, model.ErrForbiddenNotOwner) {
		t.Errorf("err = %v, want ErrForbiddenNotOwner", err)
	}
	if transfers.createdN != 0 {
		t.Error("Create must not be called when the actor isn't the owner")
	}
}

func TestInitiateTransfer_TargetIsOwner(t *testing.T) {
	srv := ownerServer()
	svc := newService(&fakeServers{server: srv}, &fakeMembers{isMember: true}, &fakeTransfers{})

	p := validInitiateParams(srv)
	p.ToUserID = testOwner

	if _, err := svc.InitiateTransfer(context.Background(), p); !errors.Is(err, model.ErrTransferTargetIsOwner) {
		t.Errorf("err = %v, want ErrTransferTargetIsOwner", err)
	}
}

// CA2: transferring to someone who isn't a member is not allowed.
func TestInitiateTransfer_TargetNotMember(t *testing.T) {
	srv := ownerServer()
	transfers := &fakeTransfers{}
	svc := newService(&fakeServers{server: srv}, &fakeMembers{isMember: false}, transfers)

	_, err := svc.InitiateTransfer(context.Background(), validInitiateParams(srv))
	if !errors.Is(err, model.ErrTransferTargetNotMember) {
		t.Errorf("err = %v, want ErrTransferTargetNotMember", err)
	}
	if transfers.createdN != 0 {
		t.Error("Create must not be called when the target isn't a member")
	}
}

func TestInitiateTransfer_ServerNotFound(t *testing.T) {
	svc := newService(&fakeServers{getErr: model.ErrServerNotFound}, &fakeMembers{isMember: true}, &fakeTransfers{})

	p := ownership_transfer_service.InitiateParams{ServerID: uuid.New(), ActorUserID: testOwner, ToUserID: testMember}
	if _, err := svc.InitiateTransfer(context.Background(), p); !errors.Is(err, model.ErrServerNotFound) {
		t.Errorf("err = %v, want ErrServerNotFound", err)
	}
}

func TestInitiateTransfer_AlreadyPending(t *testing.T) {
	srv := ownerServer()
	transfers := &fakeTransfers{createErr: model.ErrOwnershipTransferAlreadyPending}
	svc := newService(&fakeServers{server: srv}, &fakeMembers{isMember: true}, transfers)

	_, err := svc.InitiateTransfer(context.Background(), validInitiateParams(srv))
	if !errors.Is(err, model.ErrOwnershipTransferAlreadyPending) {
		t.Errorf("err = %v, want ErrOwnershipTransferAlreadyPending", err)
	}
}

func respondParams(serverID, transferID uuid.UUID, actor string) ownership_transfer_service.RespondParams {
	return ownership_transfer_service.RespondParams{ServerID: serverID, TransferID: transferID, ActorUserID: actor}
}

func pendingTransfer(serverID uuid.UUID) model.OwnershipTransfer {
	return model.OwnershipTransfer{
		ID: uuid.New(), ServerID: serverID, FromUserID: testOwner, ToUserID: testMember,
		Status: model.OwnershipTransferPending,
	}
}

// CA1: the recipient accepts and becomes the new owner (delegated to the repo;
// here we check the service wires the call through once validation passes).
func TestAcceptTransfer_Success(t *testing.T) {
	serverID := uuid.New()
	pending := pendingTransfer(serverID)
	accepted := pending
	accepted.Status = model.OwnershipTransferAccepted
	transfers := &fakeTransfers{getResult: pending, acceptResult: accepted}
	svc := newService(&fakeServers{}, &fakeMembers{}, transfers)

	got, err := svc.AcceptTransfer(context.Background(), respondParams(serverID, pending.ID, testMember))
	if err != nil {
		t.Fatalf("AcceptTransfer() error = %v, want nil", err)
	}
	if transfers.acceptCalls != 1 {
		t.Fatalf("Accept calls = %d, want 1", transfers.acceptCalls)
	}
	if got.Status != model.OwnershipTransferAccepted {
		t.Errorf("status = %q, want accepted", got.Status)
	}
}

func TestAcceptTransfer_NotFound(t *testing.T) {
	transfers := &fakeTransfers{getErr: model.ErrOwnershipTransferNotFound}
	svc := newService(&fakeServers{}, &fakeMembers{}, transfers)

	_, err := svc.AcceptTransfer(context.Background(), respondParams(uuid.New(), uuid.New(), testMember))
	if !errors.Is(err, model.ErrOwnershipTransferNotFound) {
		t.Errorf("err = %v, want ErrOwnershipTransferNotFound", err)
	}
	if transfers.acceptCalls != 0 {
		t.Error("Accept must not be called when the transfer doesn't exist")
	}
}

func TestAcceptTransfer_ServerMismatch(t *testing.T) {
	pending := pendingTransfer(uuid.New())
	transfers := &fakeTransfers{getResult: pending}
	svc := newService(&fakeServers{}, &fakeMembers{}, transfers)

	_, err := svc.AcceptTransfer(context.Background(), respondParams(uuid.New(), pending.ID, testMember))
	if !errors.Is(err, model.ErrOwnershipTransferServerMismatch) {
		t.Errorf("err = %v, want ErrOwnershipTransferServerMismatch", err)
	}
	if transfers.acceptCalls != 0 {
		t.Error("Accept must not be called on a server mismatch")
	}
}

func TestAcceptTransfer_WrongTarget_Forbidden(t *testing.T) {
	serverID := uuid.New()
	pending := pendingTransfer(serverID)
	transfers := &fakeTransfers{getResult: pending}
	svc := newService(&fakeServers{}, &fakeMembers{}, transfers)

	_, err := svc.AcceptTransfer(context.Background(), respondParams(serverID, pending.ID, "someone-else"))
	if !errors.Is(err, model.ErrOwnershipTransferWrongTarget) {
		t.Errorf("err = %v, want ErrOwnershipTransferWrongTarget", err)
	}
	if transfers.acceptCalls != 0 {
		t.Error("Accept must not be called for the wrong actor")
	}
}

func TestAcceptTransfer_NotPending(t *testing.T) {
	serverID := uuid.New()
	resolved := pendingTransfer(serverID)
	resolved.Status = model.OwnershipTransferRejected
	transfers := &fakeTransfers{getResult: resolved}
	svc := newService(&fakeServers{}, &fakeMembers{}, transfers)

	_, err := svc.AcceptTransfer(context.Background(), respondParams(serverID, resolved.ID, testMember))
	if !errors.Is(err, model.ErrOwnershipTransferNotPending) {
		t.Errorf("err = %v, want ErrOwnershipTransferNotPending", err)
	}
	if transfers.acceptCalls != 0 {
		t.Error("Accept must not be called on an already-resolved transfer")
	}
}

// CA3: the recipient rejects and ownership is left untouched (the repo is the
// one that leaves owner_id alone; here we check the service reaches Reject,
// not Accept, and only after the same validation as accept).
func TestRejectTransfer_Success(t *testing.T) {
	serverID := uuid.New()
	pending := pendingTransfer(serverID)
	rejected := pending
	rejected.Status = model.OwnershipTransferRejected
	transfers := &fakeTransfers{getResult: pending, rejectResult: rejected}
	svc := newService(&fakeServers{}, &fakeMembers{}, transfers)

	got, err := svc.RejectTransfer(context.Background(), respondParams(serverID, pending.ID, testMember))
	if err != nil {
		t.Fatalf("RejectTransfer() error = %v, want nil", err)
	}
	if transfers.rejectCalls != 1 || transfers.acceptCalls != 0 {
		t.Fatalf("reject calls = %d, accept calls = %d, want 1, 0", transfers.rejectCalls, transfers.acceptCalls)
	}
	if got.Status != model.OwnershipTransferRejected {
		t.Errorf("status = %q, want rejected", got.Status)
	}
}

func TestRejectTransfer_WrongTarget_Forbidden(t *testing.T) {
	serverID := uuid.New()
	pending := pendingTransfer(serverID)
	transfers := &fakeTransfers{getResult: pending}
	svc := newService(&fakeServers{}, &fakeMembers{}, transfers)

	_, err := svc.RejectTransfer(context.Background(), respondParams(serverID, pending.ID, testOwner))
	if !errors.Is(err, model.ErrOwnershipTransferWrongTarget) {
		t.Errorf("err = %v, want ErrOwnershipTransferWrongTarget", err)
	}
}

// Cancelling lets the owner withdraw a pending transfer, e.g. to send it to
// a different member.
func TestCancelTransfer_Success(t *testing.T) {
	serverID := uuid.New()
	pending := pendingTransfer(serverID)
	cancelled := pending
	cancelled.Status = model.OwnershipTransferCancelled
	transfers := &fakeTransfers{getResult: pending, cancelResult: cancelled}
	svc := newService(&fakeServers{}, &fakeMembers{}, transfers)

	got, err := svc.CancelTransfer(context.Background(), respondParams(serverID, pending.ID, testOwner))
	if err != nil {
		t.Fatalf("CancelTransfer() error = %v, want nil", err)
	}
	if transfers.cancelCalls != 1 {
		t.Fatalf("Cancel calls = %d, want 1", transfers.cancelCalls)
	}
	if got.Status != model.OwnershipTransferCancelled {
		t.Errorf("status = %q, want cancelled", got.Status)
	}
}

func TestCancelTransfer_Unauthenticated(t *testing.T) {
	svc := newService(&fakeServers{}, &fakeMembers{}, &fakeTransfers{})

	_, err := svc.CancelTransfer(context.Background(), respondParams(uuid.New(), uuid.New(), ""))
	if !errors.Is(err, model.ErrUnauthorized) {
		t.Errorf("err = %v, want ErrUnauthorized", err)
	}
}

func TestCancelTransfer_NotFound(t *testing.T) {
	transfers := &fakeTransfers{getErr: model.ErrOwnershipTransferNotFound}
	svc := newService(&fakeServers{}, &fakeMembers{}, transfers)

	_, err := svc.CancelTransfer(context.Background(), respondParams(uuid.New(), uuid.New(), testOwner))
	if !errors.Is(err, model.ErrOwnershipTransferNotFound) {
		t.Errorf("err = %v, want ErrOwnershipTransferNotFound", err)
	}
	if transfers.cancelCalls != 0 {
		t.Error("Cancel must not be called when the transfer doesn't exist")
	}
}

func TestCancelTransfer_ServerMismatch(t *testing.T) {
	pending := pendingTransfer(uuid.New())
	transfers := &fakeTransfers{getResult: pending}
	svc := newService(&fakeServers{}, &fakeMembers{}, transfers)

	_, err := svc.CancelTransfer(context.Background(), respondParams(uuid.New(), pending.ID, testOwner))
	if !errors.Is(err, model.ErrOwnershipTransferServerMismatch) {
		t.Errorf("err = %v, want ErrOwnershipTransferServerMismatch", err)
	}
	if transfers.cancelCalls != 0 {
		t.Error("Cancel must not be called on a server mismatch")
	}
}

// Only the initiating owner can cancel; the recipient cannot cancel their
// own incoming transfer this way (they should reject it instead).
func TestCancelTransfer_NotInitiator_Forbidden(t *testing.T) {
	serverID := uuid.New()
	pending := pendingTransfer(serverID)
	transfers := &fakeTransfers{getResult: pending}
	svc := newService(&fakeServers{}, &fakeMembers{}, transfers)

	_, err := svc.CancelTransfer(context.Background(), respondParams(serverID, pending.ID, testMember))
	if !errors.Is(err, model.ErrOwnershipTransferNotInitiator) {
		t.Errorf("err = %v, want ErrOwnershipTransferNotInitiator", err)
	}
	if transfers.cancelCalls != 0 {
		t.Error("Cancel must not be called for the wrong actor")
	}
}

func TestCancelTransfer_NotPending(t *testing.T) {
	serverID := uuid.New()
	resolved := pendingTransfer(serverID)
	resolved.Status = model.OwnershipTransferAccepted
	transfers := &fakeTransfers{getResult: resolved}
	svc := newService(&fakeServers{}, &fakeMembers{}, transfers)

	_, err := svc.CancelTransfer(context.Background(), respondParams(serverID, resolved.ID, testOwner))
	if !errors.Is(err, model.ErrOwnershipTransferNotPending) {
		t.Errorf("err = %v, want ErrOwnershipTransferNotPending", err)
	}
	if transfers.cancelCalls != 0 {
		t.Error("Cancel must not be called on an already-resolved transfer")
	}
}

func TestGetPendingTransfer_Success(t *testing.T) {
	srv := ownerServer()
	pending := pendingTransfer(srv.ID)
	transfers := &fakeTransfers{pendingResult: pending}
	svc := newService(&fakeServers{server: srv}, &fakeMembers{isMember: true}, transfers)

	got, err := svc.GetPendingTransfer(context.Background(), ownership_transfer_service.GetPendingParams{
		ServerID: srv.ID, ActorUserID: testMember,
	})
	if err != nil {
		t.Fatalf("GetPendingTransfer() error = %v, want nil", err)
	}
	if transfers.pendingCalls != 1 {
		t.Fatalf("GetPendingByServer calls = %d, want 1", transfers.pendingCalls)
	}
	if got.ID != pending.ID {
		t.Errorf("got id %v, want %v", got.ID, pending.ID)
	}
}

func TestGetPendingTransfer_NotAMember_Forbidden(t *testing.T) {
	srv := ownerServer()
	svc := newService(&fakeServers{server: srv}, &fakeMembers{isMember: false}, &fakeTransfers{})

	_, err := svc.GetPendingTransfer(context.Background(), ownership_transfer_service.GetPendingParams{
		ServerID: srv.ID, ActorUserID: "outsider",
	})
	if !errors.Is(err, model.ErrForbiddenNotMember) {
		t.Errorf("err = %v, want ErrForbiddenNotMember", err)
	}
}

func TestGetPendingTransfer_None_NotFound(t *testing.T) {
	srv := ownerServer()
	transfers := &fakeTransfers{pendingErr: model.ErrOwnershipTransferNotFound}
	svc := newService(&fakeServers{server: srv}, &fakeMembers{isMember: true}, transfers)

	_, err := svc.GetPendingTransfer(context.Background(), ownership_transfer_service.GetPendingParams{
		ServerID: srv.ID, ActorUserID: testOwner,
	})
	if !errors.Is(err, model.ErrOwnershipTransferNotFound) {
		t.Errorf("err = %v, want ErrOwnershipTransferNotFound", err)
	}
}

func TestGetPendingTransfer_Unauthenticated(t *testing.T) {
	svc := newService(&fakeServers{}, &fakeMembers{}, &fakeTransfers{})

	_, err := svc.GetPendingTransfer(context.Background(), ownership_transfer_service.GetPendingParams{ServerID: uuid.New()})
	if !errors.Is(err, model.ErrUnauthorized) {
		t.Errorf("err = %v, want ErrUnauthorized", err)
	}
}

func TestGetPendingTransfer_ServerNotFound(t *testing.T) {
	svc := newService(&fakeServers{getErr: model.ErrServerNotFound}, &fakeMembers{}, &fakeTransfers{})

	_, err := svc.GetPendingTransfer(context.Background(), ownership_transfer_service.GetPendingParams{
		ServerID: uuid.New(), ActorUserID: testMember,
	})
	if !errors.Is(err, model.ErrServerNotFound) {
		t.Errorf("err = %v, want ErrServerNotFound", err)
	}
}
