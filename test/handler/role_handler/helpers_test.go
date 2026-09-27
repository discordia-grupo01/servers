package role_handler_test

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

func (fakeServerUseCases) GetServer(context.Context, uuid.UUID) (model.Server, error) {
	return model.Server{}, nil
}

func (fakeServerUseCases) UpdateServer(context.Context, service.UpdateServerParams) (model.Server, error) {
	return model.Server{}, nil
}

func (fakeServerUseCases) GetServerImageURLs(context.Context, uuid.UUID) (iconURL, bannerURL *string, err error) {
	return nil, nil, nil
}

type fakeRoleUseCases struct {
	createErr   error
	created     role.Role
	listResult  []role.Role
	listErr     error
	gotServerID uuid.UUID
	gotListUser string

	getResult  role.Role
	getErr     error
	gotGetRole uuid.UUID
	gotGetUser string

	updateErr error
	updated   role.Role
	gotUpdate role_service.UpdateRolePermissionsParams

	setDefaultErr error
	gotSetDefault role_service.SetDefaultRoleParams

	deleteErr error
	gotDelete role_service.DeleteRoleParams

	reorderErr    error
	reorderResult []role.Role
	gotReorder    role_service.ReorderRolesParams
	reorderCalled bool
}

func (f *fakeRoleUseCases) CreateRole(_ context.Context, p role_service.CreateRoleParams) (role.Role, error) {
	f.gotServerID = p.ServerID
	if f.createErr != nil {
		return role.Role{}, f.createErr
	}
	if f.created.ID == uuid.Nil {
		f.created = role.Role{ID: uuid.New(), ServerID: p.ServerID, Name: p.Name, Color: p.Color}
	}
	return f.created, nil
}

func (f *fakeRoleUseCases) ListRoles(_ context.Context, serverID uuid.UUID, userID string) ([]role.Role, error) {
	f.gotServerID, f.gotListUser = serverID, userID
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.listResult, nil
}

func (f *fakeRoleUseCases) GetRole(_ context.Context, roleID uuid.UUID, userID string) (role.Role, error) {
	f.gotGetRole, f.gotGetUser = roleID, userID
	if f.getErr != nil {
		return role.Role{}, f.getErr
	}
	if f.getResult.ID == uuid.Nil {
		f.getResult = role.Role{ID: roleID, Name: "Fetched"}
	}
	return f.getResult, nil
}

func (f *fakeRoleUseCases) UpdateRolePermissions(_ context.Context, p role_service.UpdateRolePermissionsParams) (role.Role, error) {
	f.gotUpdate = p
	if f.updateErr != nil {
		return role.Role{}, f.updateErr
	}
	if f.updated.ID == uuid.Nil {
		f.updated = role.Role{ID: p.RoleID, Name: "Updated"}
	}
	return f.updated, nil
}

func (f *fakeRoleUseCases) SetDefaultRole(_ context.Context, p role_service.SetDefaultRoleParams) error {
	f.gotSetDefault = p
	return f.setDefaultErr
}

func (f *fakeRoleUseCases) DeleteRole(_ context.Context, p role_service.DeleteRoleParams) error {
	f.gotDelete = p
	return f.deleteErr
}

func (f *fakeRoleUseCases) ReorderRoles(_ context.Context, p role_service.ReorderRolesParams) ([]role.Role, error) {
	f.reorderCalled, f.gotReorder = true, p
	if f.reorderErr != nil {
		return nil, f.reorderErr
	}
	return f.reorderResult, nil
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

type roleJSON struct {
	ID          string   `json:"id"`
	ServerID    string   `json:"server_id"`
	Name        string   `json:"name"`
	Color       string   `json:"color"`
	Permissions []string `json:"permissions"`
	Position    int      `json:"position"`
}

func newRouter(t *testing.T, roles *fakeRoleUseCases) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	return handler.NewRouter(handler.Deps{
		Servers: fakeServerUseCases{},
		Roles:   roles,
		Auth:    middleware.AuthConfig{UserIDHeader: "X-User-Id"},
		DB:      okPinger{},
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

func decodeRole(t *testing.T, w *httptest.ResponseRecorder) roleJSON {
	t.Helper()
	var r roleJSON
	if err := json.Unmarshal(w.Body.Bytes(), &r); err != nil {
		t.Fatalf("bad role body %q: %v", w.Body.String(), err)
	}
	return r
}
