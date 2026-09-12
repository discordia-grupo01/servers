package channel_handler_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model"
)

func TestCreateChannel_Returns201(t *testing.T) {
	r := newRouter(t, &fakeChannelUseCases{})
	serverID := uuid.New()
	w := doJSON(t, r, http.MethodPost, "/v1/servers/"+serverID.String()+"/channels", testUser,
		map[string]string{"name": "general", "kind": "text"})
	if w.Code != http.StatusCreated {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	got := decodeChannel(t, w)
	if got.Name != "general" || got.Kind != "text" {
		t.Errorf("channel = %+v", got)
	}
}

func TestCreateChannel_WithCategoryID_ParsesUUID(t *testing.T) {
	fake := &fakeChannelUseCases{}
	r := newRouter(t, fake)
	serverID := uuid.New()
	categoryID := uuid.New()
	w := doJSON(t, r, http.MethodPost, "/v1/servers/"+serverID.String()+"/channels", testUser,
		map[string]any{"name": "anuncios", "kind": "text", "category_id": categoryID.String()})
	if w.Code != http.StatusCreated {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	if fake.gotCreate.CategoryID == nil || *fake.gotCreate.CategoryID != categoryID {
		t.Errorf("service got CategoryID=%v, want %v", fake.gotCreate.CategoryID, categoryID)
	}
}

func TestCreateChannel_InvalidCategoryID_BadRequest(t *testing.T) {
	r := newRouter(t, &fakeChannelUseCases{})
	w := doJSON(t, r, http.MethodPost, "/v1/servers/"+uuid.New().String()+"/channels", testUser,
		map[string]string{"name": "general", "kind": "text", "category_id": "not-a-uuid"})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	e := decodeError(t, w)
	if e.Error.Details["field"] != "category_id" {
		t.Errorf("details = %v", e.Error.Details)
	}
}

func TestCreateChannel_InvalidServerIDParam_BadRequest(t *testing.T) {
	r := newRouter(t, &fakeChannelUseCases{})
	w := doJSON(t, r, http.MethodPost, "/v1/servers/not-a-uuid/channels", testUser,
		map[string]string{"name": "general", "kind": "text"})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
}

func TestCreateChannel_DomainError_MapsToHTTPStatus(t *testing.T) {
	r := newRouter(t, &fakeChannelUseCases{createErr: model.ErrForbiddenNotOwner})
	w := doJSON(t, r, http.MethodPost, "/v1/servers/"+uuid.New().String()+"/channels", testUser,
		map[string]string{"name": "general", "kind": "text"})
	if w.Code != http.StatusForbidden {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
}

func TestCreateChannel_MissingIdentity_Returns401(t *testing.T) {
	r := newRouter(t, &fakeChannelUseCases{})
	w := doJSON(t, r, http.MethodPost, "/v1/servers/"+uuid.New().String()+"/channels", "",
		map[string]string{"name": "general", "kind": "text"})
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("code=%d", w.Code)
	}
}

func TestUpdateChannel_Returns200(t *testing.T) {
	fake := &fakeChannelUseCases{}
	r := newRouter(t, fake)
	channelID := uuid.New()
	w := doJSON(t, r, http.MethodPatch, "/v1/channels/"+channelID.String(), testUser,
		map[string]string{"name": "renamed"})
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	if fake.gotUpdate.ChannelID != channelID || fake.gotUpdate.Name == nil || *fake.gotUpdate.Name != "renamed" {
		t.Errorf("gotUpdate = %+v", fake.gotUpdate)
	}
}

func TestUpdateChannel_DomainError_MapsToHTTPStatus(t *testing.T) {
	r := newRouter(t, &fakeChannelUseCases{updateErr: model.ErrChannelNotFound})
	w := doJSON(t, r, http.MethodPatch, "/v1/channels/"+uuid.New().String(), testUser,
		map[string]string{"name": "renamed"})
	if w.Code != http.StatusNotFound {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
}

func TestDeleteChannel_Returns204(t *testing.T) {
	fake := &fakeChannelUseCases{}
	r := newRouter(t, fake)
	channelID := uuid.New()
	w := doJSON(t, r, http.MethodDelete, "/v1/channels/"+channelID.String(), testUser, nil)
	if w.Code != http.StatusNoContent {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	if fake.gotDelete.ChannelID != channelID {
		t.Errorf("gotDelete.ChannelID = %v, want %v", fake.gotDelete.ChannelID, channelID)
	}
}

func TestDeleteChannel_DomainError_MapsToHTTPStatus(t *testing.T) {
	r := newRouter(t, &fakeChannelUseCases{deleteErr: model.ErrForbiddenNotOwner})
	w := doJSON(t, r, http.MethodDelete, "/v1/channels/"+uuid.New().String(), testUser, nil)
	if w.Code != http.StatusForbidden {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
}

func TestMoveToCategory_Returns200(t *testing.T) {
	fake := &fakeChannelUseCases{}
	r := newRouter(t, fake)
	channelID := uuid.New()
	categoryID := uuid.New()
	w := doJSON(t, r, http.MethodPatch, "/v1/channels/"+channelID.String()+"/category", testUser,
		map[string]string{"category_id": categoryID.String()})
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	if fake.gotMove.CategoryID == nil || *fake.gotMove.CategoryID != categoryID {
		t.Errorf("gotMove.CategoryID = %v, want %v", fake.gotMove.CategoryID, categoryID)
	}
}

func TestMoveToCategory_NullCategoryID_MovesToNoCategory(t *testing.T) {
	fake := &fakeChannelUseCases{}
	r := newRouter(t, fake)
	channelID := uuid.New()
	w := doJSON(t, r, http.MethodPatch, "/v1/channels/"+channelID.String()+"/category", testUser,
		map[string]any{"category_id": nil})
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	if fake.gotMove.CategoryID != nil {
		t.Errorf("gotMove.CategoryID = %v, want nil", fake.gotMove.CategoryID)
	}
}

func TestReorder_Returns200(t *testing.T) {
	fake := &fakeChannelUseCases{}
	r := newRouter(t, fake)
	serverID := uuid.New()
	id1, id2 := uuid.New(), uuid.New()
	w := doJSON(t, r, http.MethodPatch, "/v1/servers/"+serverID.String()+"/channels/reorder", testUser,
		map[string]any{"channel_ids": []string{id1.String(), id2.String()}})
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	if len(fake.gotReorder.ChannelIDs) != 2 || fake.gotReorder.ChannelIDs[0] != id1 || fake.gotReorder.ChannelIDs[1] != id2 {
		t.Errorf("gotReorder.ChannelIDs = %v", fake.gotReorder.ChannelIDs)
	}
}

func TestReorder_InvalidChannelID_BadRequest(t *testing.T) {
	r := newRouter(t, &fakeChannelUseCases{})
	w := doJSON(t, r, http.MethodPatch, "/v1/servers/"+uuid.New().String()+"/channels/reorder", testUser,
		map[string]any{"channel_ids": []string{"not-a-uuid"}})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
}

func TestReorder_DomainError_MapsToHTTPStatus(t *testing.T) {
	r := newRouter(t, &fakeChannelUseCases{reorderErr: model.ErrChannelReorderInvalid})
	w := doJSON(t, r, http.MethodPatch, "/v1/servers/"+uuid.New().String()+"/channels/reorder", testUser,
		map[string]any{"channel_ids": []string{}})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
}
