package moderation_handler_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model"
)

func TestMute_DomainErrorsMapToHTTP(t *testing.T) {
	cases := map[string]struct {
		err    error
		status int
		field  string
		reason string
	}{
		"missing user_id":     {model.ErrMuteTargetRequired, http.StatusBadRequest, "user_id", "required"},
		"muting yourself":     {model.ErrCannotMuteSelf, http.StatusBadRequest, "user_id", "cannot_mute_self"},
		"target is the owner": {model.ErrCannotMuteOwner, http.StatusForbidden, "", "cannot_mute_owner"},
		"until in the past":   {model.ErrMuteUntilInvalid, http.StatusBadRequest, "until_at", "mute_until_invalid"},
		"higher role":         {model.ErrTargetOutranksActor, http.StatusForbidden, "", "target_outranks_actor"},
	}
	path := "/v1/servers/" + uuid.NewString() + "/mutes"
	body := `{"user_id":"x","until_at":"2099-01-01T00:00:00Z"}`
	for name, tc := range cases {
		w := do(t, newRouter(&fakeModeration{muteErr: tc.err}), http.MethodPost, path, testUser, body)
		if w.Code != tc.status {
			t.Errorf("%s: code=%d, want %d (body %s)", name, w.Code, tc.status, w.Body.String())
			continue
		}
		e := decodeError(t, w)
		if e.Error.Details["reason"] != tc.reason {
			t.Errorf("%s: details = %v", name, e.Error.Details)
		}
		if tc.field != "" && e.Error.Details["field"] != tc.field {
			t.Errorf("%s: details = %v", name, e.Error.Details)
		}
	}
}
