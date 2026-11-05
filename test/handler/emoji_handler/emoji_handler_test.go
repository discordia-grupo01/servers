package emoji_handler_test

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
	"github.com/discordia-grupo01/servers/internal/middleware"
	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/model/emoji"
	"github.com/discordia-grupo01/servers/internal/service/emoji_service"
)

const testUser = "user-1"

type fakeEmojis struct {
	created emoji.Emoji
	page    emoji.Page
	err     error
	calls   int

	gotCreate emoji_service.CreateParams
	gotList   emoji_service.ListParams
	gotDelete emoji_service.DeleteParams
}

func (f *fakeEmojis) Create(_ context.Context, p emoji_service.CreateParams) (emoji.Emoji, error) {
	f.calls++
	f.gotCreate = p
	return f.created, f.err
}

func (f *fakeEmojis) List(_ context.Context, p emoji_service.ListParams) (emoji.Page, error) {
	f.calls++
	f.gotList = p
	return f.page, f.err
}

func (f *fakeEmojis) Delete(_ context.Context, p emoji_service.DeleteParams) error {
	f.calls++
	f.gotDelete = p
	return f.err
}

type fakeImages struct {
	data []byte
	err  error
}

func (f fakeImages) GetEmoji(string, string) ([]byte, string, error) {
	return f.data, "image/gif", f.err
}

type okPinger struct{}

func (okPinger) Ping(context.Context) error { return nil }

func newRouter(f *fakeEmojis, images *fakeImages) *gin.Engine {
	gin.SetMode(gin.TestMode)
	deps := handler.Deps{
		Emojis: f,
		Auth:   middleware.AuthConfig{UserIDHeader: "X-User-Id"},
		DB:     okPinger{},
	}
	if images != nil {
		deps.EmojiImages = *images
	}
	return handler.NewRouter(deps)
}

func multipartBody(t *testing.T, name string, image []byte) (*bytes.Buffer, string) {
	t.Helper()
	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	if err := w.WriteField("name", name); err != nil {
		t.Fatal(err)
	}
	if image != nil {
		part, err := w.CreateFormFile("image", "emoji.gif")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(image); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return body, w.FormDataContentType()
}

func post(r http.Handler, path string, body *bytes.Buffer, contentType, user string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, body)
	req.Header.Set("Content-Type", contentType)
	if user != "" {
		req.Header.Set("X-User-Id", user)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func do(r http.Handler, method, path, user string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	if user != "" {
		req.Header.Set("X-User-Id", user)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func emojisPath(id uuid.UUID) string { return "/v1/servers/" + id.String() + "/emojis" }

func TestCreate(t *testing.T) {
	serverID := uuid.New()
	f := &fakeEmojis{created: emoji.Emoji{ID: uuid.New(), ServerID: serverID, Name: "party", Animated: true, ImageURL: "https://cdn.test/a.gif", CreatedBy: testUser}}
	body, contentType := multipartBody(t, "party", []byte("GIF89a"))

	w := post(newRouter(f, nil), emojisPath(serverID), body, contentType, testUser)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, body %s", w.Code, w.Body)
	}
	if f.gotCreate.ServerID != serverID || f.gotCreate.ActorUserID != testUser || f.gotCreate.Name != "party" || string(f.gotCreate.Image) != "GIF89a" {
		t.Errorf("params = %+v", f.gotCreate)
	}
	var got emoji.Response
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Name != "party" || !got.Animated || got.ImageURL != "https://cdn.test/a.gif" {
		t.Errorf("response = %+v", got)
	}
}

func TestCreateWithoutImageReachesTheService(t *testing.T) {
	f := &fakeEmojis{err: model.ErrEmojiImageRequired}
	body, contentType := multipartBody(t, "party", nil)

	w := post(newRouter(f, nil), emojisPath(uuid.New()), body, contentType, testUser)

	if w.Code != http.StatusBadRequest || f.gotCreate.Image != nil {
		t.Errorf("status = %d, image = %v", w.Code, f.gotCreate.Image)
	}
}

func TestCreateOversizedBodyIsRefused(t *testing.T) {
	f := &fakeEmojis{}
	body, contentType := multipartBody(t, "party", bytes.Repeat([]byte{'x'}, int(emoji.MaxImageBytes)+2<<20))

	w := post(newRouter(f, nil), emojisPath(uuid.New()), body, contentType, testUser)

	if w.Code != http.StatusBadRequest || f.calls != 0 {
		t.Fatalf("status = %d, calls = %d, body %s", w.Code, f.calls, w.Body)
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("emoji_image_too_large")) {
		t.Errorf("body = %s", w.Body)
	}
}

func TestCreateNotMultipart(t *testing.T) {
	f := &fakeEmojis{}
	w := post(newRouter(f, nil), emojisPath(uuid.New()), bytes.NewBufferString(`{}`), "application/json", testUser)
	if w.Code != http.StatusBadRequest || f.calls != 0 {
		t.Errorf("status = %d, calls = %d", w.Code, f.calls)
	}
}

func TestCreateDomainErrors(t *testing.T) {
	cases := map[string]struct {
		err    error
		status int
		reason string
	}{
		"name invalid": {model.ErrEmojiNameInvalid, http.StatusBadRequest, "emoji_name_invalid"},
		"name taken":   {model.ErrEmojiNameTaken, http.StatusConflict, "emoji_name_taken"},
		"limit":        {model.ErrEmojiLimitReached, http.StatusConflict, "emoji_limit_reached"},
		"too large":    {model.ErrEmojiImageTooLarge, http.StatusBadRequest, "emoji_image_too_large"},
		"unsupported":  {model.ErrEmojiImageUnsupported, http.StatusBadRequest, "emoji_image_unsupported"},
		"required":     {model.ErrEmojiImageRequired, http.StatusBadRequest, "emoji_image_required"},
		"forbidden":    {model.ErrForbiddenNotAdmin, http.StatusForbidden, ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			body, contentType := multipartBody(t, "party", []byte("GIF89a"))
			w := post(newRouter(&fakeEmojis{err: tc.err}, nil), emojisPath(uuid.New()), body, contentType, testUser)
			if w.Code != tc.status || !bytes.Contains(w.Body.Bytes(), []byte(tc.reason)) {
				t.Errorf("status = %d, body %s", w.Code, w.Body)
			}
		})
	}
}

func TestList(t *testing.T) {
	serverID := uuid.New()
	f := &fakeEmojis{page: emoji.Page{Emojis: []emoji.Emoji{{ID: uuid.New(), Name: "party"}}, MaxEmojis: 50}}

	w := do(newRouter(f, nil), http.MethodGet, emojisPath(serverID), testUser)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var got emoji.ListResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Emojis) != 1 || got.Emojis[0].Name != "party" || got.MaxEmojis != 50 || f.gotList.ServerID != serverID {
		t.Errorf("got %+v, params %+v", got, f.gotList)
	}
}

func TestListEmptyIsAnArray(t *testing.T) {
	w := do(newRouter(&fakeEmojis{}, nil), http.MethodGet, emojisPath(uuid.New()), testUser)
	if !bytes.Contains(w.Body.Bytes(), []byte(`"emojis":[]`)) {
		t.Errorf("body = %s", w.Body)
	}
}

func TestDelete(t *testing.T) {
	serverID, emojiID := uuid.New(), uuid.New()
	f := &fakeEmojis{}

	w := do(newRouter(f, nil), http.MethodDelete, emojisPath(serverID)+"/"+emojiID.String(), testUser)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d", w.Code)
	}
	if f.gotDelete.ServerID != serverID || f.gotDelete.EmojiID != emojiID || f.gotDelete.ActorUserID != testUser {
		t.Errorf("params = %+v", f.gotDelete)
	}
}

func TestDeleteNotFound(t *testing.T) {
	w := do(newRouter(&fakeEmojis{err: model.ErrEmojiNotFound}, nil), http.MethodDelete, emojisPath(uuid.New())+"/"+uuid.NewString(), testUser)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d", w.Code)
	}
}

func TestInvalidIDs(t *testing.T) {
	r := newRouter(&fakeEmojis{}, nil)
	for _, path := range []string{"/v1/servers/nope/emojis", emojisPath(uuid.New()) + "/nope"} {
		method := http.MethodGet
		if path != "/v1/servers/nope/emojis" {
			method = http.MethodDelete
		}
		if w := do(r, method, path, testUser); w.Code != http.StatusBadRequest {
			t.Errorf("%s %s = %d", method, path, w.Code)
		}
	}
	body, contentType := multipartBody(t, "party", []byte("GIF89a"))
	if w := post(r, "/v1/servers/nope/emojis", body, contentType, testUser); w.Code != http.StatusBadRequest {
		t.Errorf("POST = %d", w.Code)
	}
}

func TestRoutesRequireAuth(t *testing.T) {
	r := newRouter(&fakeEmojis{}, nil)
	if w := do(r, http.MethodGet, emojisPath(uuid.New()), ""); w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d", w.Code)
	}
}

func TestImageRouteOnlyExistsWithALocalStore(t *testing.T) {
	path := emojisPath(uuid.New()) + "/a.gif/image"

	if w := do(newRouter(&fakeEmojis{}, nil), http.MethodGet, path, ""); w.Code == http.StatusOK {
		t.Errorf("without a local store: status = %d", w.Code)
	}

	w := do(newRouter(&fakeEmojis{}, &fakeImages{data: []byte("GIF89a")}), http.MethodGet, path, "")
	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "image/gif" || w.Body.String() != "GIF89a" {
		t.Errorf("status = %d, type = %q, body %q", w.Code, w.Header().Get("Content-Type"), w.Body)
	}

	w = do(newRouter(&fakeEmojis{}, &fakeImages{err: model.ErrEmojiNotFound}), http.MethodGet, path, "")
	if w.Code != http.StatusNotFound {
		t.Errorf("missing image: status = %d", w.Code)
	}
}
