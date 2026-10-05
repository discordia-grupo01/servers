package voice_access_handler_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/handler/voice_access_handler"
	"github.com/discordia-grupo01/servers/internal/middleware"
	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/service/channel_service"
)

type fakeAccess struct {
	access channel_service.VoiceAccess
	err    error
	userID string
}

func (f *fakeAccess) AuthorizeVoice(_ context.Context, channelID uuid.UUID, userID string) (channel_service.VoiceAccess, error) {
	f.userID = userID
	if f.err != nil {
		return channel_service.VoiceAccess{}, f.err
	}
	f.access.ChannelID = channelID
	return f.access, nil
}

func newRouter(access *fakeAccess) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.Auth(middleware.AuthConfig{UserIDHeader: "X-User-Id"}))
	r.GET("/v1/channels/:channelId/voice-access", voice_access_handler.New(access).AuthorizeVoice)
	return r
}

func TestAuthorizeVoice_ReturnsAccessAndAuthenticatedUser(t *testing.T) {
	channelID := uuid.New()
	serverID := uuid.New()
	access := &fakeAccess{access: channel_service.VoiceAccess{ServerID: serverID, Name: "John"}}
	r := newRouter(access)

	req := httptest.NewRequest(http.MethodGet, "/v1/channels/"+channelID.String()+"/voice-access", nil)
	req.Header.Set("X-User-Id", "member-1")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if access.userID != "member-1" {
		t.Errorf("userID = %q, want member-1", access.userID)
	}
	if w.Body.String() == "" || !strings.Contains(w.Body.String(), channelID.String()) ||
		!strings.Contains(w.Body.String(), serverID.String()) || !strings.Contains(w.Body.String(), "John") ||
		!strings.Contains(w.Body.String(), "true") {
		t.Errorf("body = %s", w.Body.String())
	}
}

func TestAuthorizeVoice_InvalidChannelID_ReturnsBadRequest(t *testing.T) {
	access := &fakeAccess{}
	r := newRouter(access)

	req := httptest.NewRequest(http.MethodGet, "/v1/channels/not-a-uuid/voice-access", nil)
	req.Header.Set("X-User-Id", "member-1")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
}

func TestAuthorizeVoice_ForbiddenAccess_ReturnsForbidden(t *testing.T) {
	access := &fakeAccess{err: model.ErrForbiddenMissingPermission}
	r := newRouter(access)

	req := httptest.NewRequest(http.MethodGet, "/v1/channels/"+uuid.NewString()+"/voice-access", nil)
	req.Header.Set("X-User-Id", "member-1")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
}
