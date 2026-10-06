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
	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/handler"
	"github.com/discordia-grupo01/servers/internal/iconstore"
	"github.com/discordia-grupo01/servers/internal/middleware"
	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/model/channelperm"
	"github.com/discordia-grupo01/servers/internal/model/role"
	"github.com/discordia-grupo01/servers/internal/outbox"
	"github.com/discordia-grupo01/servers/internal/service"
)

const testUser = "user-1"

type fakeRepo struct {
	standing role.Standing

	err     error
	calls   int
	created model.Server

	listResult  []model.Server
	listErr     error
	listCalls   int
	gotListUser string

	getResult model.Server
	getErr    error
	getCalls  int

	iconURLsCalls int

	updateErr   error
	updateCalls int

	deleteErr    error
	deleteCalls  int
	deleteEvents []outbox.Event
}

func (f *fakeRepo) CreateServer(_ context.Context, s model.Server, _ []model.Category, _ []model.Channel, _ []outbox.Event) error {
	f.calls++
	if f.err != nil {
		return f.err
	}
	f.created = s
	f.getResult = s
	return nil
}

func (f *fakeRepo) ListForUser(_ context.Context, userID string) ([]model.Server, error) {
	f.listCalls++
	f.gotListUser = userID
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.listResult, nil
}

func (f *fakeRepo) GetByID(_ context.Context, _ uuid.UUID) (model.Server, error) {
	f.getCalls++
	if f.getErr != nil {
		return model.Server{}, f.getErr
	}
	return f.getResult, nil
}

func (f *fakeRepo) IconURLs(_ context.Context, _ uuid.UUID) (*string, *string, error) {
	f.iconURLsCalls++
	if f.getErr != nil {
		return nil, nil, f.getErr
	}
	return f.getResult.IconURL, f.getResult.BannerURL, nil
}

func (f *fakeRepo) UpdateServer(_ context.Context, _ uuid.UUID, name, nameKey string, iconURL, bannerURL *string) error {
	f.updateCalls++
	if f.updateErr != nil {
		return f.updateErr
	}
	f.getResult.Name, f.getResult.NameKey, f.getResult.IconURL, f.getResult.BannerURL = name, nameKey, iconURL, bannerURL
	return nil
}

func (f *fakeRepo) DeleteServer(_ context.Context, _ uuid.UUID, buildEvents func([]string, []uuid.UUID) ([]outbox.Event, error)) error {
	f.deleteCalls++
	if f.deleteErr != nil {
		return f.deleteErr
	}
	events, err := buildEvents([]string{testUser}, []uuid.UUID{uuid.New()})
	if err != nil {
		return err
	}
	f.deleteEvents = events
	return nil
}

// MemberStanding makes fakeRepo the server service's StandingReader too:
// every non-owner caller gets f.standing, whose zero value is a non-member,
// so by default only the owner administers.
func (f *fakeRepo) MemberStanding(_ context.Context, _ uuid.UUID, _ string) (role.Standing, error) {
	return f.standing, nil
}

// MemberStandings and ChannelLayers make fakeRepo the server service's
// ChannelVisibility: no standings and no overrides, so nothing is hidden from
// the owner and non-owners are non-members.
func (f *fakeRepo) MemberStandings(context.Context, string, []uuid.UUID) (map[uuid.UUID]role.Standing, error) {
	return map[uuid.UUID]role.Standing{}, nil
}

func (f *fakeRepo) ChannelLayers(context.Context, string, []uuid.UUID) (map[uuid.UUID]channelperm.Layers, error) {
	return map[uuid.UUID]channelperm.Layers{}, nil
}

type okPinger struct{}

func (okPinger) Ping(context.Context) error { return nil }

type serverJSON struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	IconURL   *string `json:"icon_url"`
	BannerURL *string `json:"banner_url"`
	OwnerID   string  `json:"owner_id"`
	Channels  []struct {
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
		Servers: service.New(repo, icons, repo, repo),
		Icons:   icons,
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

func getAsUser(t *testing.T, r http.Handler, user, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if user != "" {
		req.Header.Set("X-User-Id", user)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
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
