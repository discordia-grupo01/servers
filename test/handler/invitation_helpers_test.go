package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/handler"
	"github.com/discordia-grupo01/servers/internal/middleware"
	"github.com/discordia-grupo01/servers/internal/model"
	invitation "github.com/discordia-grupo01/servers/internal/service/invitation"
)

type fakeInvites struct {
	genErr    error
	genCalled bool
	genParams invitation.GenerateParams
	genOut    model.Invitation

	listErr    error
	listCalled bool
	listParams invitation.ListParams
	listOut    []model.Invitation

	revErr    error
	revCalled bool
	revParams invitation.RevokeParams

	joinErr    error
	joinCalled bool
	joinParams invitation.JoinParams
	joinOut    invitation.JoinResult
}

func (f *fakeInvites) GenerateInvitation(_ context.Context, p invitation.GenerateParams) (model.Invitation, error) {
	f.genCalled, f.genParams = true, p
	if f.genErr != nil {
		return model.Invitation{}, f.genErr
	}
	out := f.genOut
	if out.Code == "" {
		out.Code = "code-xyz"
	}
	out.ServerID, _ = uuid.Parse(p.ServerID)
	out.CreatedBy = p.CreatedBy
	out.MaxUses = p.MaxUses
	return out, nil
}

func (f *fakeInvites) ListInvitations(_ context.Context, p invitation.ListParams) ([]model.Invitation, error) {
	f.listCalled, f.listParams = true, p
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.listOut, nil
}

func (f *fakeInvites) RevokeInvitation(_ context.Context, p invitation.RevokeParams) error {
	f.revCalled, f.revParams = true, p
	return f.revErr
}

func (f *fakeInvites) JoinServer(_ context.Context, p invitation.JoinParams) (invitation.JoinResult, error) {
	f.joinCalled, f.joinParams = true, p
	return f.joinOut, f.joinErr
}

func newInviteRouter(t *testing.T, inv *fakeInvites) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	return handler.NewRouter(handler.Deps{
		Invitations: inv,
		Auth:        middleware.AuthConfig{UserIDHeader: "X-User-Id"},
		DB:          okPinger{},
	})
}

func doReq(t *testing.T, r http.Handler, method, path, user string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if raw, ok := body.(string); ok {
			buf.WriteString(raw)
		} else if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
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

type invitationJSON struct {
	Code      string  `json:"code"`
	URL       string  `json:"url"`
	ServerID  string  `json:"server_id"`
	CreatedBy string  `json:"created_by"`
	MaxUses   *int    `json:"max_uses"`
	Uses      int     `json:"uses"`
	ExpiresAt *string `json:"expires_at"`
	RevokedAt *string `json:"revoked_at"`
}

func decodeInvitation(t *testing.T, w *httptest.ResponseRecorder) invitationJSON {
	t.Helper()
	var out invitationJSON
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("bad body %q: %v", w.Body.String(), err)
	}
	return out
}

type joinJSON struct {
	ServerID      string `json:"server_id"`
	AlreadyMember bool   `json:"already_member"`
}

func decodeJoin(t *testing.T, w *httptest.ResponseRecorder) joinJSON {
	t.Helper()
	var out joinJSON
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("bad body %q: %v", w.Body.String(), err)
	}
	return out
}
