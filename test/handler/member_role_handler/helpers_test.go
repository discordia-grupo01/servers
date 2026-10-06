package member_role_handler_test

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
	"github.com/discordia-grupo01/servers/internal/model/role"
	"github.com/discordia-grupo01/servers/internal/service"
	"github.com/discordia-grupo01/servers/internal/service/member_role_service"
	"github.com/discordia-grupo01/servers/internal/service/role_service"
)

const testUser = "user-1"

type fakeServerUseCases struct{}

func (fakeServerUseCases) CreateServer(context.Context, service.CreateServerParams) (model.Server, error) {
	return model.Server{}, nil
}

func (fakeServerUseCases) ListMyServers(context.Context, string) ([]model.Server, error) {
	return nil, nil
}

func (fakeServerUseCases) GetServer(context.Context, uuid.UUID, string) (model.Server, error) {
	return model.Server{}, nil
}

func (fakeServerUseCases) UpdateServer(context.Context, service.UpdateServerParams) (model.Server, error) {
	return model.Server{}, nil
}

func (fakeServerUseCases) GetServerImageURLs(context.Context, uuid.UUID) (iconURL, bannerURL *string, err error) {
	return nil, nil, nil
}

type fakeRoleUseCases struct{}

func (fakeRoleUseCases) CreateRole(context.Context, role_service.CreateRoleParams) (role.Role, error) {
	return role.Role{}, nil
}
func (fakeRoleUseCases) ListRoles(context.Context, uuid.UUID, string) ([]role.Role, error) {
	return nil, nil
}
func (fakeRoleUseCases) GetRole(context.Context, uuid.UUID, string) (role.Role, error) {
	return role.Role{}, nil
}
func (fakeRoleUseCases) UpdateRolePermissions(context.Context, role_service.UpdateRolePermissionsParams) (role.Role, error) {
	return role.Role{}, nil
}
func (fakeRoleUseCases) SetDefaultRole(context.Context, role_service.SetDefaultRoleParams) error {
	return nil
}
func (fakeRoleUseCases) DeleteRole(context.Context, role_service.DeleteRoleParams) error {
	return nil
}
func (fakeRoleUseCases) ReorderRoles(context.Context, role_service.ReorderRolesParams) ([]role.Role, error) {
	return nil, nil
}

type fakeMemberRoleUseCases struct {
	assignErr    error
	assignResult role.Role
	gotAssign    member_role_service.AssignRoleParams

	removeErr error
	gotRemove member_role_service.RemoveRoleParams

	listResult []role.Role
	listErr    error
	gotList    member_role_service.MemberLookupParams

	permissionsResult []string
	permissionsErr    error
	gotPerm           member_role_service.MemberLookupParams
}

func (f *fakeMemberRoleUseCases) AssignRole(_ context.Context, p member_role_service.AssignRoleParams) (role.Role, error) {
	f.gotAssign = p
	if f.assignErr != nil {
		return role.Role{}, f.assignErr
	}
	return f.assignResult, nil
}

func (f *fakeMemberRoleUseCases) RemoveRole(_ context.Context, p member_role_service.RemoveRoleParams) error {
	f.gotRemove = p
	return f.removeErr
}

func (f *fakeMemberRoleUseCases) ListMemberRoles(_ context.Context, p member_role_service.MemberLookupParams) ([]role.Role, error) {
	f.gotList = p
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.listResult, nil
}

func (f *fakeMemberRoleUseCases) GetMemberPermissions(_ context.Context, p member_role_service.MemberLookupParams) ([]string, error) {
	f.gotPerm = p
	if f.permissionsErr != nil {
		return nil, f.permissionsErr
	}
	return f.permissionsResult, nil
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

func newRouter(t *testing.T, memberRoles *fakeMemberRoleUseCases) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	return handler.NewRouter(handler.Deps{
		Servers:     fakeServerUseCases{},
		Roles:       fakeRoleUseCases{},
		MemberRoles: memberRoles,
		Auth:        middleware.AuthConfig{UserIDHeader: "X-User-Id"},
		DB:          okPinger{},
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

func (fakeServerUseCases) DeleteServer(context.Context, service.DeleteServerParams) error {
	return nil
}
