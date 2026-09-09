package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/discordia-grupo01/servers/internal/handler"
	"github.com/discordia-grupo01/servers/internal/iconstore"
	"github.com/discordia-grupo01/servers/internal/middleware"
	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/service"
)

const testUser = "user-1"

type fakeRepo struct {
	err     error
	calls   int
	created model.Server
}

func (f *fakeRepo) CreateServer(_ context.Context, s model.Server, _ []model.Channel) error {
	f.calls++
	if f.err != nil {
		return f.err
	}
	f.created = s
	return nil
}

type okPinger struct{}

func (okPinger) Ping(context.Context) error { return nil }

type serverJSON struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	IconURL  *string `json:"icon_url"`
	OwnerID  string  `json:"owner_id"`
	Channels []struct {
		Name     string `json:"name"`
		Kind     string `json:"kind"`
		Position int    `json:"position"`
	} `json:"channels"`
}

type errJSON struct {
	Error struct {
		Code    int            `json:"code"`
		Message string         `json:"message"`
		Details map[string]any `json:"details"`
	} `json:"error"`
}

func pngBytes() []byte { return append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 64)...) }

func newRouter(t *testing.T, maxIcon int64, repoErr error) (*gin.Engine, *fakeRepo) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	repo := &fakeRepo{err: repoErr}
	icons, err := iconstore.NewLocalStore(t.TempDir(), maxIcon)
	if err != nil {
		t.Fatal(err)
	}
	r := handler.NewRouter(handler.Deps{
		Servers: service.New(repo, icons),
		Auth:    middleware.AuthConfig{UserIDHeader: "X-User-Id"},
		DB:      okPinger{},
	})
	return r, repo
}

func postCreate(t *testing.T, r http.Handler, user, name string, icon []byte) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormField("name")
	_, _ = fw.Write([]byte(name))
	if icon != nil {
		fw, _ := mw.CreateFormFile("icon", "icon.bin")
		_, _ = fw.Write(icon)
	}
	_ = mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/v1/servers", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if user != "" {
		req.Header.Set("X-User-Id", user)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func postGet(t *testing.T, r http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
	return w
}

func decodeServer(t *testing.T, w *httptest.ResponseRecorder) serverJSON {
	t.Helper()
	var s serverJSON
	if err := json.Unmarshal(w.Body.Bytes(), &s); err != nil {
		t.Fatalf("bad body %q: %v", w.Body.String(), err)
	}
	return s
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
	if e.Error.Details == nil {
		t.Error("error.details must be an object, not null")
	}
	return e
}
