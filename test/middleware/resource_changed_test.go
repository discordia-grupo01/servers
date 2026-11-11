package middleware_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/middleware"
)

type recorded struct {
	serverID uuid.UUID
	resource string
}

type fakeChanges struct {
	calls       []recorded
	recordErr   error
	channelOf   map[uuid.UUID]uuid.UUID
	categoryOf  map[uuid.UUID]uuid.UUID
	resolveFail bool
}

func (f *fakeChanges) RecordResourceChanged(_ context.Context, serverID uuid.UUID, resource string) error {
	f.calls = append(f.calls, recorded{serverID, resource})
	return f.recordErr
}

func (f *fakeChanges) ServerOfChannel(_ context.Context, id uuid.UUID) (uuid.UUID, error) {
	if f.resolveFail {
		return uuid.Nil, errors.New("not found")
	}
	return f.channelOf[id], nil
}

func (f *fakeChanges) ServerOfCategory(_ context.Context, id uuid.UUID) (uuid.UUID, error) {
	if f.resolveFail {
		return uuid.Nil, errors.New("not found")
	}
	return f.categoryOf[id], nil
}

func router(f *fakeChanges, status int) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	v1 := r.Group("/v1")
	v1.Use(middleware.ResourceChanged(f, f))
	reply := func(c *gin.Context) { c.Status(status) }
	v1.PATCH("/channels/:channelId", reply)
	v1.PATCH("/channels/:channelId/category", reply)
	v1.PATCH("/servers/:serverId/channels/reorder", reply)
	v1.POST("/servers/:serverId/categories", reply)
	v1.PATCH("/categories/:categoryId", reply)
	v1.PATCH("/servers/:serverId/categories/reorder", reply)
	v1.PATCH("/servers/:serverId/roles/reorder", reply)
	v1.PUT("/servers/:serverId/default-role", reply)
	v1.PATCH("/servers/:serverId/vanity", reply)
	v1.GET("/servers/:serverId", reply)
	v1.POST("/servers/:serverId/emojis", reply)
	return r
}

func call(r *gin.Engine, method, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(method, path, nil))
	return w
}

func TestResourceChanged_RoutesThatDoNotPublishTheirOwnEvent(t *testing.T) {
	server, channel, category := uuid.New(), uuid.New(), uuid.New()
	cases := []struct {
		method, path, resource string
	}{
		{http.MethodPatch, "/v1/channels/" + channel.String(), "channels"},
		{http.MethodPatch, "/v1/channels/" + channel.String() + "/category", "channels"},
		{http.MethodPatch, "/v1/servers/" + server.String() + "/channels/reorder", "channels"},
		{http.MethodPost, "/v1/servers/" + server.String() + "/categories", "channels"},
		{http.MethodPatch, "/v1/categories/" + category.String(), "channels"},
		{http.MethodPatch, "/v1/servers/" + server.String() + "/categories/reorder", "channels"},
		{http.MethodPatch, "/v1/servers/" + server.String() + "/roles/reorder", "roles"},
		{http.MethodPut, "/v1/servers/" + server.String() + "/default-role", "roles"},
		{http.MethodPatch, "/v1/servers/" + server.String() + "/vanity", "server"},
	}
	for _, tc := range cases {
		f := &fakeChanges{channelOf: map[uuid.UUID]uuid.UUID{channel: server}, categoryOf: map[uuid.UUID]uuid.UUID{category: server}}
		w := call(router(f, http.StatusOK), tc.method, tc.path)
		if w.Code != http.StatusOK {
			t.Fatalf("%s %s: code = %d", tc.method, tc.path, w.Code)
		}
		if len(f.calls) != 1 || f.calls[0].serverID != server || f.calls[0].resource != tc.resource {
			t.Errorf("%s %s: recorded %+v, want %s of %s", tc.method, tc.path, f.calls, tc.resource, server)
		}
	}
}

func TestResourceChanged_OnlyWhenTheRequestSucceeded(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusForbidden, http.StatusNotFound, http.StatusConflict, http.StatusInternalServerError} {
		f := &fakeChanges{}
		call(router(f, status), http.MethodPatch, "/v1/servers/"+uuid.NewString()+"/vanity")
		if len(f.calls) != 0 {
			t.Errorf("status %d recorded %+v, want nothing", status, f.calls)
		}
	}
}

func TestResourceChanged_IgnoresReadsAndRoutesThatHaveTheirOwnEvent(t *testing.T) {
	f := &fakeChanges{}
	r := router(f, http.StatusOK)

	call(r, http.MethodGet, "/v1/servers/"+uuid.NewString())
	call(r, http.MethodPost, "/v1/servers/"+uuid.NewString()+"/emojis")

	if len(f.calls) != 0 {
		t.Errorf("recorded %+v, want nothing", f.calls)
	}
}

func TestResourceChanged_AFailureToRecordNeverChangesTheAnswer(t *testing.T) {
	server := uuid.New()
	f := &fakeChanges{recordErr: errors.New("db down")}

	w := call(router(f, http.StatusOK), http.MethodPatch, "/v1/servers/"+server.String()+"/vanity")

	if w.Code != http.StatusOK || len(f.calls) != 1 {
		t.Errorf("code = %d, calls = %d, want 200 and one attempt", w.Code, len(f.calls))
	}
}

func TestResourceChanged_AServerThatCannotBeResolvedIsSkipped(t *testing.T) {
	f := &fakeChanges{resolveFail: true}
	w := call(router(f, http.StatusOK), http.MethodPatch, "/v1/channels/"+uuid.NewString())

	if w.Code != http.StatusOK || len(f.calls) != 0 {
		t.Errorf("code = %d, calls = %d, want 200 and none", w.Code, len(f.calls))
	}

	w = call(router(&fakeChanges{}, http.StatusOK), http.MethodPatch, "/v1/channels/no-es-un-uuid")
	if w.Code != http.StatusOK {
		t.Errorf("code = %d, want 200", w.Code)
	}
}
