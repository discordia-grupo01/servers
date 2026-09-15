package category_handler_test

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
	"github.com/discordia-grupo01/servers/internal/service"
	"github.com/discordia-grupo01/servers/internal/service/category_service"
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

type fakeCategoryUseCases struct {
	createErr error
	created   model.Category
	gotCreate category_service.CreateCategoryParams

	updateErr error
	updated   model.Category
	gotUpdate category_service.UpdateCategoryParams

	reorderErr error
	gotReorder category_service.ReorderCategoriesParams
}

func (f *fakeCategoryUseCases) CreateCategory(_ context.Context, p category_service.CreateCategoryParams) (model.Category, error) {
	f.gotCreate = p
	if f.createErr != nil {
		return model.Category{}, f.createErr
	}
	if f.created.ID == uuid.Nil {
		f.created = model.Category{ID: uuid.New(), ServerID: p.ServerID, Name: p.Name}
	}
	return f.created, nil
}

func (f *fakeCategoryUseCases) UpdateCategory(_ context.Context, p category_service.UpdateCategoryParams) (model.Category, error) {
	f.gotUpdate = p
	if f.updateErr != nil {
		return model.Category{}, f.updateErr
	}
	if f.updated.ID == uuid.Nil {
		f.updated = model.Category{ID: p.CategoryID, Name: p.Name}
	}
	return f.updated, nil
}

func (f *fakeCategoryUseCases) ReorderCategories(_ context.Context, p category_service.ReorderCategoriesParams) error {
	f.gotReorder = p
	return f.reorderErr
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

type categoryJSON struct {
	ID       string `json:"id"`
	ServerID string `json:"server_id"`
	Name     string `json:"name"`
	Position int    `json:"position"`
}

func newRouter(t *testing.T, categories *fakeCategoryUseCases) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	return handler.NewRouter(handler.Deps{
		Servers:    fakeServerUseCases{},
		Categories: categories,
		Auth:       middleware.AuthConfig{UserIDHeader: "X-User-Id"},
		DB:         okPinger{},
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

func decodeCategory(t *testing.T, w *httptest.ResponseRecorder) categoryJSON {
	t.Helper()
	var c categoryJSON
	if err := json.Unmarshal(w.Body.Bytes(), &c); err != nil {
		t.Fatalf("bad category body %q: %v", w.Body.String(), err)
	}
	return c
}
