package handler_test

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/discordia-grupo01/servers/internal/model"
)

const oneMB = 1 << 20

func TestCreate_Returns201(t *testing.T) {
	r, _ := newRouter(t, oneMB, nil)
	w := postCreate(t, r, testUser, "Mi Comunidad", nil)
	if w.Code != http.StatusCreated {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
}

func TestCreate_BodyHasNameOwnerAndTwoChannels(t *testing.T) {
	r, _ := newRouter(t, oneMB, nil)
	w := postCreate(t, r, testUser, "Mi Comunidad", nil)
	s := decodeServer(t, w)
	if s.Name != "Mi Comunidad" {
		t.Errorf("name = %q", s.Name)
	}
	if s.OwnerID != testUser {
		t.Errorf("owner_id = %q, want %q", s.OwnerID, testUser)
	}
	if len(s.Channels) != 2 {
		t.Fatalf("channels = %d, want 2", len(s.Channels))
	}
}

func TestCreate_DefaultChannelsAreOneTextAndOneVoice(t *testing.T) {
	r, _ := newRouter(t, oneMB, nil)
	s := decodeServer(t, postCreate(t, r, testUser, "Mi Comunidad", nil))
	kinds := map[string]bool{}
	for _, c := range s.Channels {
		kinds[c.Kind] = true
	}
	if !kinds["text"] || !kinds["voice"] {
		t.Errorf("channel kinds = %v, want text+voice", kinds)
	}
}

func TestCreate_WithValidPNG_Returns201AndIconURL(t *testing.T) {
	r, _ := newRouter(t, oneMB, nil)
	w := postCreate(t, r, testUser, "Con Icono", pngBytes())
	if w.Code != http.StatusCreated {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	s := decodeServer(t, w)
	if s.IconURL == nil || !strings.HasSuffix(*s.IconURL, "/icon") {
		t.Errorf("icon_url = %v", s.IconURL)
	}
}

func TestCreate_WithoutIcon_IconURLIsNull(t *testing.T) {
	r, _ := newRouter(t, oneMB, nil)
	s := decodeServer(t, postCreate(t, r, testUser, "Sin Icono", nil))
	if s.IconURL != nil {
		t.Errorf("icon_url = %v, want null", *s.IconURL)
	}
}

func TestCreate_TrimsTheName(t *testing.T) {
	r, _ := newRouter(t, oneMB, nil)
	s := decodeServer(t, postCreate(t, r, testUser, "   Mi Comunidad   ", nil))
	if s.Name != "Mi Comunidad" {
		t.Errorf("name = %q", s.Name)
	}
}

func TestCreate_AcceptsNameAtMaxLength(t *testing.T) {
	r, _ := newRouter(t, oneMB, nil)
	w := postCreate(t, r, testUser, strings.Repeat("a", model.ServerNameMaxLen), nil)
	if w.Code != http.StatusCreated {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
}

func assertNameRejected(t *testing.T, name, wantReason string) {
	t.Helper()
	r, _ := newRouter(t, oneMB, nil)
	w := postCreate(t, r, testUser, name, nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("name %q: code=%d body=%s", name, w.Code, w.Body.String())
	}
	e := decodeError(t, w)
	if e.Error.Details["field"] != "name" {
		t.Errorf("name %q: details.field = %v", name, e.Error.Details["field"])
	}
	if e.Error.Details["reason"] != wantReason {
		t.Errorf("name %q: details.reason = %v, want %q", name, e.Error.Details["reason"], wantReason)
	}
}

func TestCreate_EmptyName_Returns400(t *testing.T) {
	assertNameRejected(t, "", "name_required")
}

func TestCreate_WhitespaceOnlyName_Returns400(t *testing.T) {
	assertNameRejected(t, "    ", "name_required")
}

func TestCreate_ShortName_Returns400(t *testing.T) {
	assertNameRejected(t, "a", "name_too_short")
}

func TestCreate_LongName_Returns400(t *testing.T) {
	assertNameRejected(t, strings.Repeat("a", model.ServerNameMaxLen+1), "name_too_long")
}

func TestCreate_NameWithNullByte_Returns400(t *testing.T) {
	assertNameRejected(t, "bad\x00name", "name_invalid_chars")
}

func TestCreate_NameWithNewline_Returns400(t *testing.T) {
	assertNameRejected(t, "bad\nname", "name_invalid_chars")
}

func TestCreate_NameWithTab_Returns400(t *testing.T) {
	assertNameRejected(t, "bad\tname", "name_invalid_chars")
}

func TestCreate_UnsupportedIconType_Returns400(t *testing.T) {
	r, repo := newRouter(t, oneMB, nil)
	gif := append([]byte("GIF87a"), make([]byte, 32)...)
	w := postCreate(t, r, testUser, "Con Gif", gif)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	if e := decodeError(t, w); e.Error.Details["reason"] != "icon_unsupported_type" {
		t.Errorf("reason = %v", e.Error.Details["reason"])
	}
	if repo.calls != 0 {
		t.Error("server was created with an invalid icon")
	}
}

func TestCreate_IconOverConfiguredMax_Returns400(t *testing.T) {
	r, _ := newRouter(t, 16, nil) // tope 16 bytes
	w := postCreate(t, r, testUser, "Icono Grande", pngBytes())
	if w.Code != http.StatusBadRequest {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	if e := decodeError(t, w); e.Error.Details["reason"] != "icon_too_large" {
		t.Errorf("reason = %v", e.Error.Details["reason"])
	}
}

func TestCreate_DuplicateName_Returns409(t *testing.T) {
	r, _ := newRouter(t, oneMB, model.ErrNameTaken)
	w := postCreate(t, r, testUser, "Repetido", nil)
	if w.Code != http.StatusConflict {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	if e := decodeError(t, w); e.Error.Details["reason"] != "name_taken" {
		t.Errorf("reason = %v", e.Error.Details["reason"])
	}
}

func TestCreate_RepositoryError_Returns500(t *testing.T) {
	r, _ := newRouter(t, oneMB, errors.New("db down"))
	w := postCreate(t, r, testUser, "Mi Comunidad", nil)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("code=%d", w.Code)
	}
}

func TestCreate_MissingIdentity_Returns401(t *testing.T) {
	r, _ := newRouter(t, oneMB, nil)
	w := postCreate(t, r, "", "Mi Comunidad", nil)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("code=%d", w.Code)
	}
}

func TestCreate_ErrorBodyShape(t *testing.T) {
	r, _ := newRouter(t, oneMB, nil)
	w := postCreate(t, r, testUser, "", nil)
	e := decodeError(t, w)
	if e.Error.Code != 400 || e.Error.Message == "" {
		t.Errorf("error = %+v", e.Error)
	}
}

func TestHealthEndpoints(t *testing.T) {
	r, _ := newRouter(t, oneMB, nil)
	for _, path := range []string{"/livez", "/readyz", "/"} {
		w := postGet(t, r, path)
		if w.Code != http.StatusOK {
			t.Errorf("%s => %d", path, w.Code)
		}
	}
}
