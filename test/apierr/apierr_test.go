package apierr_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/discordia-grupo01/servers/internal/apierr"
	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/test/errshape"
)

func serveErr(t *testing.T, fn func(c *gin.Context)) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/x", fn)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))
	return w
}

func TestRespondError_NilDetailsBecomesEmptyObject(t *testing.T) {
	w := serveErr(t, func(c *gin.Context) {
		apierr.RespondError(c, http.StatusTeapot, "SOME_CODE", "mensaje", nil)
	})
	if got := errshape.Envelope(t, w.Body.Bytes()); got != "SOME_CODE" {
		t.Errorf("code = %q", got)
	}
}

func TestWriteDomainError_MapsEveryCaseToAStableCode(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   string
		reason string // details.reason que se mantiene (RF5), vacío si no tenía
	}{
		{model.ErrNameTaken, 409, "NAME_TAKEN", "name_taken"},
		{model.ErrNameRequired, 400, "NAME_REQUIRED", "name_required"},
		{model.ErrServerNotFound, 404, "SERVER_NOT_FOUND", ""},
		{model.ErrForbiddenNotOwner, 403, "NOT_SERVER_OWNER", ""},
		{model.ErrOwnerCannotLeave, 409, "OWNER_MUST_TRANSFER_OR_DELETE", "owner_must_transfer_or_delete"},
		{model.ErrRoleNotFound, 404, "ROLE_NOT_FOUND", ""},
		{model.ErrChannelNotFound, 404, "CHANNEL_NOT_FOUND", ""},
		{model.ErrChannelNameTaken, 400, "CHANNEL_NAME_TAKEN", "name_taken"},
		{model.ErrCategoryNameTaken, 400, "CATEGORY_NAME_TAKEN", "name_taken"},
		{model.ErrForbiddenMissingPermission, 403, "MISSING_PERMISSION", "missing_permission"},
		{model.ErrAlreadyBanned, 409, "ALREADY_BANNED", "already_banned"},
		{model.ErrAlreadyMuted, 409, "ALREADY_MUTED", "already_muted"},
		{model.ErrInvitationExpired, 404, "INVITATION_INVALID", "invitation_invalid"},
		{model.ErrUserBanned, 403, "USER_BANNED", "user_banned"},
		{model.ErrUnauthorized, 401, "UNAUTHORIZED", ""},
		{model.ErrDeleteConfirmationMismatch, 400, "DELETE_CONFIRMATION_MISMATCH", "confirmation_mismatch"},
		{http.ErrAbortHandler, 500, "INTERNAL_ERROR", ""},
	}
	for _, tc := range cases {
		w := serveErr(t, func(c *gin.Context) { apierr.WriteDomainError(c, tc.err) })
		if w.Code != tc.status {
			t.Errorf("%v: status = %d, want %d", tc.err, w.Code, tc.status)
		}
		if got := errshape.Envelope(t, w.Body.Bytes()); got != tc.code {
			t.Errorf("%v: code = %q, want %q", tc.err, got, tc.code)
		}
		if tc.reason != "" && !contains(w.Body.String(), `"reason":"`+tc.reason+`"`) {
			t.Errorf("%v: falta details.reason %q en %s", tc.err, tc.reason, w.Body.String())
		}
	}
}

func TestWriteDomainError_UnknownErrorDoesNotLeakInternals(t *testing.T) {
	w := serveErr(t, func(c *gin.Context) {
		apierr.WriteDomainError(c, errString("pq: relation secret_table does not exist"))
	})
	if w.Code != 500 || contains(w.Body.String(), "secret_table") {
		t.Errorf("status=%d body=%s", w.Code, w.Body.String())
	}
	errshape.Envelope(t, w.Body.Bytes())
}

func TestParseUUIDParam_InvalidIsInvalidParameter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/x/:id", func(c *gin.Context) { apierr.ParseUUIDParam(c, "id") })
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x/no-uuid", nil))
	if w.Code != 400 || errshape.Envelope(t, w.Body.Bytes()) != "INVALID_PARAMETER" {
		t.Errorf("status=%d body=%s", w.Code, w.Body.String())
	}
	if !contains(w.Body.String(), `"param":"id"`) || !contains(w.Body.String(), `"reason":"invalid_uuid"`) {
		t.Errorf("details: %s", w.Body.String())
	}
}

type errString string

func (e errString) Error() string { return string(e) }

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
