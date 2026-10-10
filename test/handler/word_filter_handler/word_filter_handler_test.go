package word_filter_handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/handler"
	"github.com/discordia-grupo01/servers/internal/middleware"
	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/model/moderation"
	"github.com/discordia-grupo01/servers/internal/service/word_filter_service"
)

const testUser = "mod-1"

type fakeWordFilter struct {
	words      []string
	err        error
	gotList    word_filter_service.ListParams
	gotReplace word_filter_service.ReplaceParams
	replaced   bool
}

func (f *fakeWordFilter) List(_ context.Context, p word_filter_service.ListParams) ([]string, error) {
	f.gotList = p
	return f.words, f.err
}

func (f *fakeWordFilter) Replace(_ context.Context, p word_filter_service.ReplaceParams) ([]string, error) {
	f.gotReplace = p
	f.replaced = true
	return f.words, f.err
}

type okPinger struct{}

func (okPinger) Ping(context.Context) error { return nil }

func newRouter(f *fakeWordFilter) *gin.Engine {
	gin.SetMode(gin.TestMode)
	return handler.NewRouter(handler.Deps{
		WordFilter: f,
		Auth:       middleware.AuthConfig{UserIDHeader: "X-User-Id"},
		DB:         okPinger{},
	})
}

func do(r http.Handler, method, path, user, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	if user != "" {
		req.Header.Set("X-User-Id", user)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func path(serverID uuid.UUID) string { return "/v1/servers/" + serverID.String() + "/banned-words" }

func decodeWords(t *testing.T, w *httptest.ResponseRecorder) []string {
	t.Helper()
	var body struct {
		Words []string `json:"words"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("bad body %q: %v", w.Body.String(), err)
	}
	return body.Words
}

func TestList_ReturnsTheWords(t *testing.T) {
	serverID := uuid.New()
	f := &fakeWordFilter{words: []string{"tonto", "no sirve"}}

	w := do(newRouter(f), http.MethodGet, path(serverID), testUser, "")

	if w.Code != http.StatusOK || !reflect.DeepEqual(decodeWords(t, w), f.words) {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	if f.gotList.ServerID != serverID || f.gotList.ActorUserID != testUser {
		t.Errorf("service got %+v", f.gotList)
	}
}

func TestReplace_SendsTheWholeListAndReturnsTheSavedOne(t *testing.T) {
	serverID := uuid.New()
	f := &fakeWordFilter{words: []string{"tonto"}}

	w := do(newRouter(f), http.MethodPut, path(serverID), testUser, `{"words":["Tonto ","tonto"]}`)

	if w.Code != http.StatusOK || !reflect.DeepEqual(decodeWords(t, w), []string{"tonto"}) {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	want := word_filter_service.ReplaceParams{ServerID: serverID, ActorUserID: testUser, Words: []string{"Tonto ", "tonto"}}
	if !reflect.DeepEqual(f.gotReplace, want) {
		t.Errorf("service got %+v", f.gotReplace)
	}
}

func TestReplace_AnEmptyListIsValid(t *testing.T) {
	f := &fakeWordFilter{words: []string{}}

	w := do(newRouter(f), http.MethodPut, path(uuid.New()), testUser, `{"words":[]}`)

	if w.Code != http.StatusOK || w.Body.String() != `{"words":[]}` || !f.replaced {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
}

func TestReplace_RejectsABodyWithoutTheList(t *testing.T) {
	for _, body := range []string{`{}`, `{"words":null}`, `{"words":`, `{"words":"tonto"}`} {
		f := &fakeWordFilter{}
		w := do(newRouter(f), http.MethodPut, path(uuid.New()), testUser, body)
		if w.Code != http.StatusBadRequest || f.replaced {
			t.Errorf("%s: code=%d replaced=%v", body, w.Code, f.replaced)
		}
	}
}

func TestReplace_MissingIdentity_Returns401(t *testing.T) {
	w := do(newRouter(&fakeWordFilter{}), http.MethodPut, path(uuid.New()), "", `{"words":[]}`)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("code=%d", w.Code)
	}
}

func TestDomainErrorsMapToHTTP(t *testing.T) {
	cases := map[string]struct {
		err    error
		status int
		reason string
	}{
		"missing MANAGE_MESSAGES": {model.ErrForbiddenMissingPermission, http.StatusForbidden, "missing_permission"},
		"blank word":              {moderation.ErrBannedWordEmpty, http.StatusBadRequest, "word_empty"},
		"word too long":           {moderation.ErrBannedWordTooLong, http.StatusBadRequest, "word_too_long"},
		"control characters":      {moderation.ErrBannedWordInvalidChars, http.StatusBadRequest, "word_invalid_chars"},
		"more than 100":           {moderation.ErrTooManyBannedWords, http.StatusBadRequest, "too_many_words"},
		"server not found":        {model.ErrServerNotFound, http.StatusNotFound, ""},
	}
	for name, tc := range cases {
		w := do(newRouter(&fakeWordFilter{err: tc.err}), http.MethodPut, path(uuid.New()), testUser, `{"words":["x"]}`)
		if w.Code != tc.status {
			t.Errorf("%s: code=%d, want %d", name, w.Code, tc.status)
			continue
		}
		var e struct {
			Error struct {
				Details map[string]any `json:"details"`
			} `json:"error"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &e)
		if tc.reason != "" && e.Error.Details["reason"] != tc.reason {
			t.Errorf("%s: details = %v", name, e.Error.Details)
		}
	}
}
