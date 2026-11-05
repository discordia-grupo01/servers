package handler_test

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model"
)

func patchServer(t *testing.T, r http.Handler, user, serverID string, fields map[string]string, files map[string][]byte) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for k, v := range fields {
		fw, _ := mw.CreateFormField(k)
		_, _ = fw.Write([]byte(v))
	}
	for field, data := range files {
		fw, _ := mw.CreateFormFile(field, field+".bin")
		_, _ = fw.Write(data)
	}
	_ = mw.Close()

	req := httptest.NewRequest(http.MethodPatch, "/v1/servers/"+serverID, &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if user != "" {
		req.Header.Set("X-User-Id", user)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// CA1: a valid edit by the owner persists (the repo is called) and the
// change is reflected in the response.
func TestUpdateServer_NameChange_Returns200(t *testing.T) {
	r, repo := newRouter(t, oneMB, nil)
	id := uuid.New()
	repo.getResult = model.Server{ID: id, Name: "Original", OwnerID: testUser}

	w := patchServer(t, r, testUser, id.String(), map[string]string{"name": "Updated"}, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	if got := decodeServer(t, w); got.Name != "Updated" {
		t.Errorf("name = %q, want %q", got.Name, "Updated")
	}
	if repo.updateCalls != 1 {
		t.Errorf("UpdateServer calls = %d, want 1", repo.updateCalls)
	}
}

func TestUpdateServer_IconChange_IsServedBackAfterwards(t *testing.T) {
	r, repo := newRouter(t, oneMB, nil)
	id := uuid.New()
	repo.getResult = model.Server{ID: id, Name: "Con Icono", OwnerID: testUser}

	w := patchServer(t, r, testUser, id.String(), nil, map[string][]byte{"icon": pngBytes()})
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}

	get := getAsUser(t, r, testUser, "/v1/servers/"+id.String()+"/icon")
	if get.Code != http.StatusOK {
		t.Fatalf("icon not servable after update: code=%d", get.Code)
	}
}

func TestUpdateServer_IconChangedTwice_AlwaysServesTheLatestOne(t *testing.T) {
	r, repo := newRouter(t, oneMB, nil)
	id := uuid.New()
	repo.getResult = model.Server{ID: id, Name: "Con Icono", OwnerID: testUser}

	firstIcon := pngBytes()
	if w := patchServer(t, r, testUser, id.String(), nil, map[string][]byte{"icon": firstIcon}); w.Code != http.StatusOK {
		t.Fatalf("first update: code=%d body=%s", w.Code, w.Body.String())
	}
	firstGet := getAsUser(t, r, testUser, "/v1/servers/"+id.String()+"/icon")
	if firstGet.Code != http.StatusOK || firstGet.Body.String() != string(firstIcon) {
		t.Fatalf("first icon not served correctly: code=%d", firstGet.Code)
	}

	secondIcon := append([]byte("\xff\xd8\xff\xe0"), make([]byte, 64)...) // JPEG, distinto contenido
	if w := patchServer(t, r, testUser, id.String(), nil, map[string][]byte{"icon": secondIcon}); w.Code != http.StatusOK {
		t.Fatalf("second update: code=%d body=%s", w.Code, w.Body.String())
	}
	secondGet := getAsUser(t, r, testUser, "/v1/servers/"+id.String()+"/icon")
	if secondGet.Code != http.StatusOK {
		t.Fatalf("second icon not servable: code=%d", secondGet.Code)
	}
	if secondGet.Body.String() != string(secondIcon) {
		t.Error("GET /icon served the old content after a second update")
	}
	if secondGet.Header().Get("Content-Type") != "image/jpeg" {
		t.Errorf("content-type = %q, want image/jpeg", secondGet.Header().Get("Content-Type"))
	}
}

// CA2: a non-administrator can't edit the server's config, and nothing is persisted.
func TestUpdateServer_NotAdmin_Returns403(t *testing.T) {
	r, repo := newRouter(t, oneMB, nil)
	id := uuid.New()
	repo.getResult = model.Server{ID: id, Name: "Original", OwnerID: testUser}

	w := patchServer(t, r, "someone-else", id.String(), map[string]string{"name": "Hijacked"}, nil)
	if w.Code != http.StatusForbidden {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	if repo.updateCalls != 0 {
		t.Error("UpdateServer must not be called for a non-administrator")
	}
}

func TestUpdateServer_MissingIdentity_Returns401(t *testing.T) {
	r, _ := newRouter(t, oneMB, nil)
	w := patchServer(t, r, "", uuid.New().String(), map[string]string{"name": "Nope"}, nil)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("code=%d", w.Code)
	}
}

func TestUpdateServer_UnknownServer_Returns404(t *testing.T) {
	r, repo := newRouter(t, oneMB, nil)
	repo.getErr = model.ErrServerNotFound

	w := patchServer(t, r, testUser, uuid.New().String(), map[string]string{"name": "Nope"}, nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
}

func TestUpdateServer_InvalidServerID_Returns400(t *testing.T) {
	r, _ := newRouter(t, oneMB, nil)
	w := patchServer(t, r, testUser, "not-a-uuid", map[string]string{"name": "Nope"}, nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("code=%d", w.Code)
	}
}

// CA3: invalid data is rejected and nothing is persisted.
func TestUpdateServer_TooShortName_Returns400AndDoesNotPersist(t *testing.T) {
	r, repo := newRouter(t, oneMB, nil)
	id := uuid.New()
	repo.getResult = model.Server{ID: id, Name: "Original", OwnerID: testUser}

	w := patchServer(t, r, testUser, id.String(), map[string]string{"name": "a"}, nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	if e := decodeError(t, w); e.Error.Details["reason"] != "name_too_short" {
		t.Errorf("reason = %v", e.Error.Details["reason"])
	}
	if repo.updateCalls != 0 {
		t.Error("UpdateServer must not be called with an invalid name")
	}
}

func TestUpdateServer_UnsupportedIconType_Returns400(t *testing.T) {
	r, repo := newRouter(t, oneMB, nil)
	id := uuid.New()
	repo.getResult = model.Server{ID: id, Name: "Original", OwnerID: testUser}
	bmp := append([]byte("BM"), make([]byte, 32)...)

	w := patchServer(t, r, testUser, id.String(), nil, map[string][]byte{"icon": bmp})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	if e := decodeError(t, w); e.Error.Details["reason"] != "icon_unsupported_type" {
		t.Errorf("reason = %v", e.Error.Details["reason"])
	}
	if repo.updateCalls != 0 {
		t.Error("UpdateServer must not be called when the icon is rejected")
	}
}

func TestUpdateServer_DuplicateName_Returns409(t *testing.T) {
	r, repo := newRouter(t, oneMB, nil)
	id := uuid.New()
	repo.getResult = model.Server{ID: id, Name: "Original", OwnerID: testUser}
	repo.updateErr = model.ErrNameTaken

	w := patchServer(t, r, testUser, id.String(), map[string]string{"name": "Taken"}, nil)
	if w.Code != http.StatusConflict {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
}

// -- banner --

func TestUpdateServer_BannerUpload_IsServedBackPublicly(t *testing.T) {
	r, repo := newRouter(t, oneMB, nil)
	id := uuid.New()
	repo.getResult = model.Server{ID: id, Name: "Con Banner", OwnerID: testUser, BoostLevel: model.BoostLevelBanner}

	w := patchServer(t, r, testUser, id.String(), nil, map[string][]byte{"banner": pngBytes()})
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	if got := decodeServer(t, w); got.BannerURL == nil {
		t.Error("banner_url = nil, want it set")
	}

	// No X-User-Id: banner URLs must be publicly reachable.
	get := getAsUser(t, r, "", "/v1/servers/"+id.String()+"/banner")
	if get.Code != http.StatusOK {
		t.Fatalf("banner not publicly servable: code=%d body=%s", get.Code, get.Body.String())
	}
}

func TestUpdateServer_RemoveBanner_ClearsItAndDeletesFromStore(t *testing.T) {
	r, repo := newRouter(t, oneMB, nil)
	id := uuid.New()
	repo.getResult = model.Server{ID: id, Name: "Con Banner", OwnerID: testUser, BoostLevel: model.BoostLevelBanner}
	if w := patchServer(t, r, testUser, id.String(), nil, map[string][]byte{"banner": pngBytes()}); w.Code != http.StatusOK {
		t.Fatalf("setup: code=%d", w.Code)
	}

	// Simulate the server row now carrying the banner set above, as GetByID
	// would return it on the next request.
	bannerURL := "/v1/servers/" + id.String() + "/banner"
	repo.getResult.BannerURL = &bannerURL

	w := patchServer(t, r, testUser, id.String(), map[string]string{"remove_banner": "true"}, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	if got := decodeServer(t, w); got.BannerURL != nil {
		t.Errorf("banner_url = %v, want nil", got.BannerURL)
	}

	get := getAsUser(t, r, "", "/v1/servers/"+id.String()+"/banner")
	if get.Code != http.StatusNotFound {
		t.Errorf("banner still servable after removal: code=%d", get.Code)
	}
}

func TestGetBanner_NoBannerSet_Returns404(t *testing.T) {
	r, _ := newRouter(t, oneMB, nil)
	w := getAsUser(t, r, "", "/v1/servers/"+uuid.New().String()+"/banner")
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestGetBanner_InvalidServerID_Returns400(t *testing.T) {
	r, _ := newRouter(t, oneMB, nil)
	w := getAsUser(t, r, testUser, "/v1/servers/not-a-uuid/banner")
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

// GET /icon remains public even when the server has no uploaded icon.
func TestGetIcon_NoAuth_IsStillServed(t *testing.T) {
	r, _ := newRouter(t, oneMB, nil)
	s := decodeServer(t, postCreate(t, r, testUser, "Sin Icono", nil))

	w := getAsUser(t, r, "", "/v1/servers/"+s.ID+"/icon")
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for a server without an icon", w.Code)
	}
}

func TestUpdateServer_BannerWithoutLevel_Returns403(t *testing.T) {
	r, repo := newRouter(t, oneMB, nil)
	id := uuid.New()
	repo.getResult = model.Server{ID: id, Name: "Sin Nivel", OwnerID: testUser, BoostLevel: 1}

	w := patchServer(t, r, testUser, id.String(), nil, map[string][]byte{"banner": pngBytes()})
	if w.Code != http.StatusForbidden {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	e := decodeError(t, w)
	if e.Error.Details["reason"] != "boost_level_required" {
		t.Errorf("reason = %v", e.Error.Details["reason"])
	}
	if e.Error.Details["required_level"] != float64(model.BoostLevelBanner) {
		t.Errorf("required_level = %v", e.Error.Details["required_level"])
	}
}

// A level the server lost hides what it stored, without erasing it.
func TestGetServer_HidesWhatTheLevelNoLongerUnlocks(t *testing.T) {
	r, repo := newRouter(t, oneMB, nil)
	id := uuid.New()
	icon, banner, vanity := "/icon?v=1", "/banner?v=1", "mi-servidor"
	repo.getResult = model.Server{
		ID: id, Name: "Bajo de nivel", OwnerID: testUser, BoostCount: 2,
		IconURL: &icon, IconAnimated: true, BannerURL: &banner, VanityCode: &vanity,
	}

	w := getAsUser(t, r, testUser, "/v1/servers/"+id.String())
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"icon_url", "banner_url", "vanity_code"} {
		if got[field] != nil {
			t.Errorf("%s = %v, want null", field, got[field])
		}
	}
	if got["boost_count"] != float64(2) || got["boost_level"] != float64(0) {
		t.Errorf("boost_count=%v boost_level=%v", got["boost_count"], got["boost_level"])
	}

	repo.getResult.BoostLevel = 3
	w = getAsUser(t, r, testUser, "/v1/servers/"+id.String())
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	for field, want := range map[string]string{"icon_url": icon, "banner_url": banner, "vanity_code": vanity} {
		if got[field] != want {
			t.Errorf("%s = %v, want %q at level 3", field, got[field], want)
		}
	}
}
