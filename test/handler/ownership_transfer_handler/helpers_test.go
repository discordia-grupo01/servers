package ownership_transfer_handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/discordia-grupo01/servers/internal/handler"
	"github.com/discordia-grupo01/servers/internal/middleware"
	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/service/ownership_transfer_service"
)

const testUser = "user-1"

type fakeUseCases struct {
	initiateErr    error
	initiateResult model.OwnershipTransfer
	gotInitiate    ownership_transfer_service.InitiateParams
	initiateCalls  int

	pendingErr    error
	pendingResult model.OwnershipTransfer
	gotPending    ownership_transfer_service.GetPendingParams
	pendingCalls  int

	acceptErr    error
	acceptResult model.OwnershipTransfer
	gotAccept    ownership_transfer_service.RespondParams
	acceptCalls  int

	rejectErr    error
	rejectResult model.OwnershipTransfer
	gotReject    ownership_transfer_service.RespondParams
	rejectCalls  int

	cancelErr    error
	cancelResult model.OwnershipTransfer
	gotCancel    ownership_transfer_service.RespondParams
	cancelCalls  int
}

func (f *fakeUseCases) InitiateTransfer(_ context.Context, p ownership_transfer_service.InitiateParams) (model.OwnershipTransfer, error) {
	f.initiateCalls++
	f.gotInitiate = p
	if f.initiateErr != nil {
		return model.OwnershipTransfer{}, f.initiateErr
	}
	return f.initiateResult, nil
}

func (f *fakeUseCases) GetPendingTransfer(_ context.Context, p ownership_transfer_service.GetPendingParams) (model.OwnershipTransfer, error) {
	f.pendingCalls++
	f.gotPending = p
	if f.pendingErr != nil {
		return model.OwnershipTransfer{}, f.pendingErr
	}
	return f.pendingResult, nil
}

func (f *fakeUseCases) AcceptTransfer(_ context.Context, p ownership_transfer_service.RespondParams) (model.OwnershipTransfer, error) {
	f.acceptCalls++
	f.gotAccept = p
	if f.acceptErr != nil {
		return model.OwnershipTransfer{}, f.acceptErr
	}
	return f.acceptResult, nil
}

func (f *fakeUseCases) RejectTransfer(_ context.Context, p ownership_transfer_service.RespondParams) (model.OwnershipTransfer, error) {
	f.rejectCalls++
	f.gotReject = p
	if f.rejectErr != nil {
		return model.OwnershipTransfer{}, f.rejectErr
	}
	return f.rejectResult, nil
}

func (f *fakeUseCases) CancelTransfer(_ context.Context, p ownership_transfer_service.RespondParams) (model.OwnershipTransfer, error) {
	f.cancelCalls++
	f.gotCancel = p
	if f.cancelErr != nil {
		return model.OwnershipTransfer{}, f.cancelErr
	}
	return f.cancelResult, nil
}

type okPinger struct{}

func (okPinger) Ping(context.Context) error { return nil }

type transferJSON struct {
	ID         string  `json:"id"`
	ServerID   string  `json:"server_id"`
	FromUserID string  `json:"from_user_id"`
	ToUserID   string  `json:"to_user_id"`
	Status     string  `json:"status"`
	ResolvedAt *string `json:"resolved_at"`
}

type errJSON struct {
	Error struct {
		Code    int            `json:"code"`
		Message string         `json:"message"`
		Details map[string]any `json:"details"`
	} `json:"error"`
}

func newRouter(t *testing.T, uc *fakeUseCases) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	return handler.NewRouter(handler.Deps{
		OwnershipTransfers: uc,
		Auth:               middleware.AuthConfig{UserIDHeader: "X-User-Id"},
		DB:                 okPinger{},
	})
}

func doJSON(t *testing.T, r http.Handler, method, path, user string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	if user != "" {
		req.Header.Set("X-User-Id", user)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func decodeTransfer(t *testing.T, w *httptest.ResponseRecorder) transferJSON {
	t.Helper()
	var tr transferJSON
	if err := json.Unmarshal(w.Body.Bytes(), &tr); err != nil {
		t.Fatalf("bad body %q: %v", w.Body.String(), err)
	}
	return tr
}

func decodeError(t *testing.T, w *httptest.ResponseRecorder) errJSON {
	t.Helper()
	var e errJSON
	if err := json.Unmarshal(w.Body.Bytes(), &e); err != nil {
		t.Fatalf("bad error body %q: %v", w.Body.String(), err)
	}
	if e.Error.Code != w.Code {
		t.Errorf("error.code=%d but HTTP status=%d", e.Error.Code, w.Code)
	}
	return e
}
