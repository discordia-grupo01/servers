package membership_handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/discordia-grupo01/servers/internal/handler"
	"github.com/discordia-grupo01/servers/internal/middleware"
	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/service/membership_service"
)

const testUser = "user-1"

type fakeMembershipUseCases struct {
	err       error
	calls     int
	gotParams membership_service.LeaveParams

	listMembers   []model.Member
	listTotal     int
	listErr       error
	listCalls     int
	gotListParams membership_service.ListMembersParams
}

func (f *fakeMembershipUseCases) LeaveServer(_ context.Context, p membership_service.LeaveParams) error {
	f.calls++
	f.gotParams = p
	return f.err
}

func (f *fakeMembershipUseCases) ListMembers(_ context.Context, p membership_service.ListMembersParams) ([]model.Member, int, error) {
	f.listCalls++
	f.gotListParams = p
	if f.listErr != nil {
		return nil, 0, f.listErr
	}
	return f.listMembers, f.listTotal, nil
}

type okPinger struct{}

func (okPinger) Ping(context.Context) error { return nil }

type errJSON struct {
	Error struct {
		Code    int            `json:"code"`
		Message string         `json:"message"`
		Details map[string]any `json:"details"`
	} `json:"error"`
}

func newRouter(t *testing.T, uc *fakeMembershipUseCases) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	return handler.NewRouter(handler.Deps{
		Membership: uc,
		Auth:       middleware.AuthConfig{UserIDHeader: "X-User-Id"},
		DB:         okPinger{},
	})
}

func doDelete(t *testing.T, r http.Handler, path, user string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodDelete, path, nil)
	if user != "" {
		req.Header.Set("X-User-Id", user)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func doGet(t *testing.T, r http.Handler, path, user string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if user != "" {
		req.Header.Set("X-User-Id", user)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
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
