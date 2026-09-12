package ownership_transfer_handler_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model"
)

func initiatePath(serverID string) string {
	return "/v1/servers/" + serverID + "/ownership-transfers"
}

func acceptPath(serverID, transferID string) string {
	return "/v1/servers/" + serverID + "/ownership-transfers/" + transferID + "/accept"
}

func rejectPath(serverID, transferID string) string {
	return "/v1/servers/" + serverID + "/ownership-transfers/" + transferID + "/reject"
}

func pendingPath(serverID string) string {
	return "/v1/servers/" + serverID + "/ownership-transfers/pending"
}

func TestInitiate_NoAuth_401(t *testing.T) {
	uc := &fakeUseCases{}
	r := newRouter(t, uc)

	w := doJSON(t, r, http.MethodPost, initiatePath(uuid.New().String()), "", map[string]string{"to_user_id": "member-1"})
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
	if uc.initiateCalls != 0 {
		t.Error("use case must not be called without a caller id")
	}
}

func TestInitiate_InvalidServerID_400(t *testing.T) {
	uc := &fakeUseCases{}
	r := newRouter(t, uc)

	w := doJSON(t, r, http.MethodPost, initiatePath("not-a-uuid"), testUser, map[string]string{"to_user_id": "member-1"})
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
	if uc.initiateCalls != 0 {
		t.Error("use case must not be called with an invalid serverId")
	}
}

func TestInitiate_Success_201(t *testing.T) {
	serverID := uuid.New()
	uc := &fakeUseCases{initiateResult: model.OwnershipTransfer{
		ID: uuid.New(), ServerID: serverID, FromUserID: testUser, ToUserID: "member-1",
		Status: model.OwnershipTransferPending,
	}}
	r := newRouter(t, uc)

	w := doJSON(t, r, http.MethodPost, initiatePath(serverID.String()), testUser, map[string]string{"to_user_id": "member-1"})
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201, body=%s", w.Code, w.Body.String())
	}
	if uc.gotInitiate.ActorUserID != testUser || uc.gotInitiate.ToUserID != "member-1" {
		t.Errorf("params = %+v", uc.gotInitiate)
	}
	tr := decodeTransfer(t, w)
	if tr.Status != "pending" {
		t.Errorf("status in body = %q, want pending", tr.Status)
	}
}

func TestInitiate_TargetNotMember_400(t *testing.T) {
	uc := &fakeUseCases{initiateErr: model.ErrTransferTargetNotMember}
	r := newRouter(t, uc)

	w := doJSON(t, r, http.MethodPost, initiatePath(uuid.New().String()), testUser, map[string]string{"to_user_id": "stranger"})
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
	e := decodeError(t, w)
	if e.Error.Details["reason"] != "not_a_member" {
		t.Errorf("details.reason = %v, want not_a_member", e.Error.Details["reason"])
	}
}

func TestInitiate_NotOwner_403(t *testing.T) {
	uc := &fakeUseCases{initiateErr: model.ErrForbiddenNotOwner}
	r := newRouter(t, uc)

	w := doJSON(t, r, http.MethodPost, initiatePath(uuid.New().String()), testUser, map[string]string{"to_user_id": "member-1"})
	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", w.Code)
	}
}

func TestInitiate_AlreadyPending_409(t *testing.T) {
	uc := &fakeUseCases{initiateErr: model.ErrOwnershipTransferAlreadyPending}
	r := newRouter(t, uc)

	w := doJSON(t, r, http.MethodPost, initiatePath(uuid.New().String()), testUser, map[string]string{"to_user_id": "member-1"})
	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409", w.Code)
	}
}

func TestAccept_Success_200(t *testing.T) {
	serverID, transferID := uuid.New(), uuid.New()
	uc := &fakeUseCases{acceptResult: model.OwnershipTransfer{
		ID: transferID, ServerID: serverID, FromUserID: "owner-1", ToUserID: testUser,
		Status: model.OwnershipTransferAccepted,
	}}
	r := newRouter(t, uc)

	w := doJSON(t, r, http.MethodPost, acceptPath(serverID.String(), transferID.String()), testUser, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	if uc.gotAccept.ActorUserID != testUser {
		t.Errorf("ActorUserID = %q, want %q", uc.gotAccept.ActorUserID, testUser)
	}
	tr := decodeTransfer(t, w)
	if tr.Status != "accepted" {
		t.Errorf("status in body = %q, want accepted", tr.Status)
	}
}

func TestAccept_WrongTarget_403(t *testing.T) {
	uc := &fakeUseCases{acceptErr: model.ErrOwnershipTransferWrongTarget}
	r := newRouter(t, uc)

	w := doJSON(t, r, http.MethodPost, acceptPath(uuid.New().String(), uuid.New().String()), testUser, nil)
	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", w.Code)
	}
}

func TestAccept_NotFound_404(t *testing.T) {
	uc := &fakeUseCases{acceptErr: model.ErrOwnershipTransferNotFound}
	r := newRouter(t, uc)

	w := doJSON(t, r, http.MethodPost, acceptPath(uuid.New().String(), uuid.New().String()), testUser, nil)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestAccept_InvalidTransferID_400(t *testing.T) {
	uc := &fakeUseCases{}
	r := newRouter(t, uc)

	w := doJSON(t, r, http.MethodPost, acceptPath(uuid.New().String(), "not-a-uuid"), testUser, nil)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
	if uc.acceptCalls != 0 {
		t.Error("use case must not be called with an invalid transferId")
	}
}

// CA3: reject leaves ownership untouched -- verified at the service/repo
// level; here we just check the handler wires reject (not accept) and
// reports the resulting status.
func TestReject_Success_200(t *testing.T) {
	serverID, transferID := uuid.New(), uuid.New()
	uc := &fakeUseCases{rejectResult: model.OwnershipTransfer{
		ID: transferID, ServerID: serverID, FromUserID: "owner-1", ToUserID: testUser,
		Status: model.OwnershipTransferRejected,
	}}
	r := newRouter(t, uc)

	w := doJSON(t, r, http.MethodPost, rejectPath(serverID.String(), transferID.String()), testUser, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	if uc.acceptCalls != 0 || uc.rejectCalls != 1 {
		t.Errorf("accept calls = %d, reject calls = %d, want 0, 1", uc.acceptCalls, uc.rejectCalls)
	}
	tr := decodeTransfer(t, w)
	if tr.Status != "rejected" {
		t.Errorf("status in body = %q, want rejected", tr.Status)
	}
}

func TestReject_NotPending_409(t *testing.T) {
	uc := &fakeUseCases{rejectErr: model.ErrOwnershipTransferNotPending}
	r := newRouter(t, uc)

	w := doJSON(t, r, http.MethodPost, rejectPath(uuid.New().String(), uuid.New().String()), testUser, nil)
	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409", w.Code)
	}
}

func TestGetPending_NoAuth_401(t *testing.T) {
	uc := &fakeUseCases{}
	r := newRouter(t, uc)

	w := doJSON(t, r, http.MethodGet, pendingPath(uuid.New().String()), "", nil)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
	if uc.pendingCalls != 0 {
		t.Error("use case must not be called without a caller id")
	}
}

func TestGetPending_InvalidServerID_400(t *testing.T) {
	uc := &fakeUseCases{}
	r := newRouter(t, uc)

	w := doJSON(t, r, http.MethodGet, pendingPath("not-a-uuid"), testUser, nil)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestGetPending_Success_200(t *testing.T) {
	serverID := uuid.New()
	pending := model.OwnershipTransfer{
		ID: uuid.New(), ServerID: serverID, FromUserID: "owner-1", ToUserID: testUser,
		Status: model.OwnershipTransferPending,
	}
	uc := &fakeUseCases{pendingResult: pending}
	r := newRouter(t, uc)

	w := doJSON(t, r, http.MethodGet, pendingPath(serverID.String()), testUser, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	if uc.gotPending.ServerID != serverID || uc.gotPending.ActorUserID != testUser {
		t.Errorf("params = %+v, want ServerID=%v ActorUserID=%q", uc.gotPending, serverID, testUser)
	}
	tr := decodeTransfer(t, w)
	if tr.Status != "pending" || tr.ToUserID != testUser {
		t.Errorf("body = %+v, want status=pending to_user_id=%q", tr, testUser)
	}
}

func TestGetPending_None_404(t *testing.T) {
	uc := &fakeUseCases{pendingErr: model.ErrOwnershipTransferNotFound}
	r := newRouter(t, uc)

	w := doJSON(t, r, http.MethodGet, pendingPath(uuid.New().String()), testUser, nil)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestGetPending_NotAMember_403(t *testing.T) {
	uc := &fakeUseCases{pendingErr: model.ErrForbiddenNotMember}
	r := newRouter(t, uc)

	w := doJSON(t, r, http.MethodGet, pendingPath(uuid.New().String()), testUser, nil)
	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", w.Code)
	}
}
