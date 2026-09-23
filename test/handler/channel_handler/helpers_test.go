package channel_handler_test

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
	"github.com/discordia-grupo01/servers/internal/service/channel_service"
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

type fakeChannelUseCases struct {
	createErr error
	created   model.Channel
	gotCreate channel_service.CreateChannelParams

	updateErr error
	updated   model.Channel
	gotUpdate channel_service.UpdateChannelParams

	deleteErr error
	gotDelete channel_service.DeleteChannelParams

	moveErr error
	moved   model.Channel
	gotMove channel_service.MoveChannelToCategoryParams

	reorderErr error
	gotReorder channel_service.ReorderChannelsParams
}

func (f *fakeChannelUseCases) CreateChannel(_ context.Context, p channel_service.CreateChannelParams) (model.Channel, error) {
	f.gotCreate = p
	if f.createErr != nil {
		return model.Channel{}, f.createErr
	}
	if f.created.ID == uuid.Nil {
		f.created = model.Channel{ID: uuid.New(), ServerID: p.ServerID, Name: p.Name, Kind: model.ChannelKind(p.Kind), CategoryID: p.CategoryID}
	}
	return f.created, nil
}

func (f *fakeChannelUseCases) UpdateChannel(_ context.Context, p channel_service.UpdateChannelParams) (model.Channel, error) {
	f.gotUpdate = p
	if f.updateErr != nil {
		return model.Channel{}, f.updateErr
	}
	if f.updated.ID == uuid.Nil {
		f.updated = model.Channel{ID: p.ChannelID}
	}
	return f.updated, nil
}

func (f *fakeChannelUseCases) DeleteChannel(_ context.Context, p channel_service.DeleteChannelParams) error {
	f.gotDelete = p
	return f.deleteErr
}

func (f *fakeChannelUseCases) MoveChannelToCategory(_ context.Context, p channel_service.MoveChannelToCategoryParams) (model.Channel, error) {
	f.gotMove = p
	if f.moveErr != nil {
		return model.Channel{}, f.moveErr
	}
	if f.moved.ID == uuid.Nil {
		f.moved = model.Channel{ID: p.ChannelID, CategoryID: p.CategoryID}
	}
	return f.moved, nil
}

func (f *fakeChannelUseCases) ReorderChannels(_ context.Context, p channel_service.ReorderChannelsParams) error {
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

type channelJSON struct {
	ID         string  `json:"id"`
	ServerID   string  `json:"server_id"`
	Name       string  `json:"name"`
	Kind       string  `json:"kind"`
	Position   int     `json:"position"`
	CategoryID *string `json:"category_id"`
	Topic      *string `json:"topic"`
}

func newRouter(t *testing.T, channels *fakeChannelUseCases) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	return handler.NewRouter(handler.Deps{
		Servers:  fakeServerUseCases{},
		Channels: channels,
		Auth:     middleware.AuthConfig{UserIDHeader: "X-User-Id"},
		DB:       okPinger{},
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

func decodeChannel(t *testing.T, w *httptest.ResponseRecorder) channelJSON {
	t.Helper()
	var c channelJSON
	if err := json.Unmarshal(w.Body.Bytes(), &c); err != nil {
		t.Fatalf("bad channel body %q: %v", w.Body.String(), err)
	}
	return c
}
