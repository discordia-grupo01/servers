package category_handler_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model"
)

func TestCreateCategory_Returns201(t *testing.T) {
	r := newRouter(t, &fakeCategoryUseCases{})
	serverID := uuid.New()
	w := doJSON(t, r, http.MethodPost, "/v1/servers/"+serverID.String()+"/categories", testUser,
		map[string]string{"name": "Texto"})
	if w.Code != http.StatusCreated {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	got := decodeCategory(t, w)
	if got.Name != "Texto" {
		t.Errorf("category = %+v", got)
	}
}

func TestCreateCategory_InvalidServerIDParam_BadRequest(t *testing.T) {
	r := newRouter(t, &fakeCategoryUseCases{})
	w := doJSON(t, r, http.MethodPost, "/v1/servers/not-a-uuid/categories", testUser,
		map[string]string{"name": "Texto"})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
}

func TestCreateCategory_DomainError_MapsToHTTPStatus(t *testing.T) {
	r := newRouter(t, &fakeCategoryUseCases{createErr: model.ErrForbiddenNotOwner})
	w := doJSON(t, r, http.MethodPost, "/v1/servers/"+uuid.New().String()+"/categories", testUser,
		map[string]string{"name": "Texto"})
	if w.Code != http.StatusForbidden {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
}

func TestCreateCategory_MissingIdentity_Returns401(t *testing.T) {
	r := newRouter(t, &fakeCategoryUseCases{})
	w := doJSON(t, r, http.MethodPost, "/v1/servers/"+uuid.New().String()+"/categories", "",
		map[string]string{"name": "Texto"})
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("code=%d", w.Code)
	}
}

func TestUpdateCategory_Returns200(t *testing.T) {
	fake := &fakeCategoryUseCases{}
	r := newRouter(t, fake)
	categoryID := uuid.New()
	w := doJSON(t, r, http.MethodPatch, "/v1/categories/"+categoryID.String(), testUser,
		map[string]string{"name": "renamed"})
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	if fake.gotUpdate.CategoryID != categoryID || fake.gotUpdate.Name != "renamed" {
		t.Errorf("gotUpdate = %+v", fake.gotUpdate)
	}
}

func TestUpdateCategory_InvalidCategoryIDParam_BadRequest(t *testing.T) {
	r := newRouter(t, &fakeCategoryUseCases{})
	w := doJSON(t, r, http.MethodPatch, "/v1/categories/not-a-uuid", testUser,
		map[string]string{"name": "renamed"})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
}

func TestUpdateCategory_DomainError_MapsToHTTPStatus(t *testing.T) {
	r := newRouter(t, &fakeCategoryUseCases{updateErr: model.ErrForbiddenNotOwner})
	w := doJSON(t, r, http.MethodPatch, "/v1/categories/"+uuid.New().String(), testUser,
		map[string]string{"name": "renamed"})
	if w.Code != http.StatusForbidden {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
}

func TestDeleteCategory_Returns204(t *testing.T) {
	fake := &fakeCategoryUseCases{}
	r := newRouter(t, fake)
	categoryID := uuid.New()
	w := doJSON(t, r, http.MethodDelete, "/v1/categories/"+categoryID.String(), testUser, nil)
	if w.Code != http.StatusNoContent {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	if fake.gotDelete.CategoryID != categoryID {
		t.Errorf("gotDelete.CategoryID = %v, want %v", fake.gotDelete.CategoryID, categoryID)
	}
}

func TestDeleteCategory_DomainError_MapsToHTTPStatus(t *testing.T) {
	r := newRouter(t, &fakeCategoryUseCases{deleteErr: model.ErrForbiddenNotOwner})
	w := doJSON(t, r, http.MethodDelete, "/v1/categories/"+uuid.New().String(), testUser, nil)
	if w.Code != http.StatusForbidden {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
}

func TestReorderCategories_Returns200(t *testing.T) {
	fake := &fakeCategoryUseCases{}
	r := newRouter(t, fake)
	serverID := uuid.New()
	id1, id2 := uuid.New(), uuid.New()
	w := doJSON(t, r, http.MethodPatch, "/v1/servers/"+serverID.String()+"/categories/reorder", testUser,
		map[string]any{"category_ids": []string{id2.String(), id1.String()}})
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	if len(fake.gotReorder.CategoryIDs) != 2 || fake.gotReorder.CategoryIDs[0] != id2 || fake.gotReorder.CategoryIDs[1] != id1 {
		t.Errorf("gotReorder.CategoryIDs = %v", fake.gotReorder.CategoryIDs)
	}
}

func TestReorderCategories_InvalidCategoryID_BadRequest(t *testing.T) {
	r := newRouter(t, &fakeCategoryUseCases{})
	w := doJSON(t, r, http.MethodPatch, "/v1/servers/"+uuid.New().String()+"/categories/reorder", testUser,
		map[string]any{"category_ids": []string{"not-a-uuid"}})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
}

func TestReorderCategories_DomainError_MapsToHTTPStatus(t *testing.T) {
	r := newRouter(t, &fakeCategoryUseCases{reorderErr: model.ErrCategoryReorderInvalid})
	w := doJSON(t, r, http.MethodPatch, "/v1/servers/"+uuid.New().String()+"/categories/reorder", testUser,
		map[string]any{"category_ids": []string{}})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
}
