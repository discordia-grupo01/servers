package boost_handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/handler/boost_handler"
	"github.com/discordia-grupo01/servers/internal/model"
)

type fakeUseCases struct {
	summary model.BoostSummary
	code    *string
	err     error
	gotCode *string
}

func (f *fakeUseCases) Summary(context.Context, uuid.UUID, string) (model.BoostSummary, error) {
	return f.summary, f.err
}

func (f *fakeUseCases) SetVanity(_ context.Context, _ uuid.UUID, _ string, code *string) (*string, error) {
	f.gotCode = code
	return f.code, f.err
}

func newRouter(uc *fakeUseCases) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := boost_handler.New(uc)
	r.GET("/servers/:serverId/boosts", h.Summary)
	r.PATCH("/servers/:serverId/vanity", h.SetVanity)
	return r
}

func do(r *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	return w
}

func TestSummary_Response(t *testing.T) {
	r := newRouter(&fakeUseCases{summary: model.SummarizeBoosts(7)})
	w := do(r, http.MethodGet, "/servers/"+uuid.NewString()+"/boosts", "")
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	var got boost_handler.SummaryResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.BoostCount != 7 || got.Level != 1 || len(got.PerksUnlocked) != 2 ||
		got.NextLevel == nil || got.NextLevel.Level != 2 || got.NextLevel.BoostsRequired != 15 || got.NextLevel.BoostsMissing != 8 {
		t.Errorf("got %+v", got)
	}
}

func TestSummary_LastLevelHasNoNext(t *testing.T) {
	r := newRouter(&fakeUseCases{summary: model.SummarizeBoosts(30)})
	w := do(r, http.MethodGet, "/servers/"+uuid.NewString()+"/boosts", "")
	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if next, present := got["next_level"]; !present || next != nil {
		t.Errorf("next_level = %v, present = %v", next, present)
	}
}

func TestSummary_NoPerksIsAnEmptyList(t *testing.T) {
	r := newRouter(&fakeUseCases{summary: model.SummarizeBoosts(0)})
	w := do(r, http.MethodGet, "/servers/"+uuid.NewString()+"/boosts", "")
	if !strings.Contains(w.Body.String(), `"perks_unlocked":[]`) {
		t.Errorf("body = %s", w.Body.String())
	}
}

func TestSummary_Errors(t *testing.T) {
	r := newRouter(&fakeUseCases{err: model.ErrForbiddenNotMember})
	if w := do(r, http.MethodGet, "/servers/"+uuid.NewString()+"/boosts", ""); w.Code != http.StatusForbidden {
		t.Errorf("not a member: code=%d", w.Code)
	}
	if w := do(r, http.MethodGet, "/servers/not-a-uuid/boosts", ""); w.Code != http.StatusBadRequest {
		t.Errorf("bad id: code=%d", w.Code)
	}
}

func TestSetVanity_SetAndClear(t *testing.T) {
	code := "mi-servidor"
	uc := &fakeUseCases{code: &code}
	r := newRouter(uc)

	w := do(r, http.MethodPatch, "/servers/"+uuid.NewString()+"/vanity", `{"code":"Mi-Servidor"}`)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"vanity_code":"mi-servidor"`) {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	if uc.gotCode == nil || *uc.gotCode != "Mi-Servidor" {
		t.Errorf("got code %v", uc.gotCode)
	}

	uc.code = nil
	w = do(r, http.MethodPatch, "/servers/"+uuid.NewString()+"/vanity", `{"code":null}`)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"vanity_code":null`) {
		t.Fatalf("clear: code=%d body=%s", w.Code, w.Body.String())
	}
	if uc.gotCode != nil {
		t.Errorf("got code %v, want nil", *uc.gotCode)
	}
}

func TestSetVanity_Errors(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		body   string
		status int
		field  string
		want   any
	}{
		{"bad body", nil, `{`, http.StatusBadRequest, "", nil},
		{"invalid", model.ErrVanityCodeInvalid, `{"code":"x"}`, http.StatusBadRequest, "code", "vanity_code_invalid"},
		{"taken", model.ErrVanityCodeTaken, `{"code":"abc"}`, http.StatusConflict, "code", "vanity_code_taken"},
		{"no level", &model.BoostLevelRequiredError{Required: 3}, `{"code":"abc"}`, http.StatusForbidden, "reason", "boost_level_required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRouter(&fakeUseCases{err: tt.err})
			w := do(r, http.MethodPatch, "/servers/"+uuid.NewString()+"/vanity", tt.body)
			if w.Code != tt.status {
				t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
			}
			if tt.field == "" {
				return
			}
			if !strings.Contains(w.Body.String(), `"`+tt.want.(string)+`"`) {
				t.Errorf("body = %s", w.Body.String())
			}
		})
	}
}
