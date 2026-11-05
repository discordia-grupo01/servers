package report_handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/handler"
	"github.com/discordia-grupo01/servers/internal/middleware"
	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/model/moderation"
	"github.com/discordia-grupo01/servers/internal/model/report"
	"github.com/discordia-grupo01/servers/internal/service/report_service"
)

const testUser = "user-1"

type fakeReports struct {
	created report.Report
	page    report.Page
	closed  report.Report
	err     error

	gotCreate report_service.CreateParams
	gotList   report_service.ListParams
	gotClose  report_service.CloseParams
	calls     int
}

func (f *fakeReports) Create(_ context.Context, p report_service.CreateParams) (report.Report, error) {
	f.calls++
	f.gotCreate = p
	return f.created, f.err
}

func (f *fakeReports) List(_ context.Context, p report_service.ListParams) (report.Page, error) {
	f.calls++
	f.gotList = p
	return f.page, f.err
}

func (f *fakeReports) Close(_ context.Context, p report_service.CloseParams) (report.Report, error) {
	f.calls++
	f.gotClose = p
	return f.closed, f.err
}

type okPinger struct{}

func (okPinger) Ping(context.Context) error { return nil }

func newRouter(f *fakeReports) *gin.Engine {
	gin.SetMode(gin.TestMode)
	return handler.NewRouter(handler.Deps{
		Reports: f,
		Auth:    middleware.AuthConfig{UserIDHeader: "X-User-Id"},
		DB:      okPinger{},
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

func basePath(serverID uuid.UUID) string { return "/v1/servers/" + serverID.String() + "/reports" }

type errorBody struct {
	Error struct {
		Code    int            `json:"code"`
		Message string         `json:"message"`
		Details map[string]any `json:"details"`
	} `json:"error"`
}

func decodeError(t *testing.T, w *httptest.ResponseRecorder) errorBody {
	t.Helper()
	var body errorBody
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("bad error body %q: %v", w.Body.String(), err)
	}
	return body
}

func TestCreate_Message_Returns201AndPassesTheTarget(t *testing.T) {
	serverID, channelID, messageID := uuid.New(), uuid.New(), uuid.New()
	f := &fakeReports{created: report.Report{
		ID: uuid.New(), ServerID: serverID, Kind: report.KindMessage, ReporterID: testUser,
		ChannelID: &channelID, MessageID: &messageID, Reason: "spam", Status: report.StatusPending,
		CreatedAt: time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC),
	}}

	w := do(newRouter(f), http.MethodPost, basePath(serverID), testUser,
		`{"kind":"message","channel_id":"`+channelID.String()+`","message_id":"`+messageID.String()+`","reason":"spam"}`)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, body %s", w.Code, w.Body.String())
	}
	var body report.Response
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("bad body: %v", err)
	}
	if body.Kind != "message" || body.Status != "pending" || body.ReporterID != testUser || body.Reason != "spam" {
		t.Errorf("body = %+v", body)
	}
	if body.ChannelID == nil || *body.ChannelID != channelID.String() || body.MessageID == nil || *body.MessageID != messageID.String() {
		t.Errorf("target = %v / %v", body.ChannelID, body.MessageID)
	}
	got := f.gotCreate
	if got.ServerID != serverID || got.ActorUserID != testUser || got.Kind != "message" || got.Reason != "spam" {
		t.Errorf("params = %+v", got)
	}
	if got.ChannelID == nil || *got.ChannelID != channelID || got.MessageID == nil || *got.MessageID != messageID {
		t.Errorf("ids = %v / %v", got.ChannelID, got.MessageID)
	}
}

func TestCreate_User_Returns201(t *testing.T) {
	serverID := uuid.New()
	target := "user-2"
	f := &fakeReports{created: report.Report{
		ID: uuid.New(), ServerID: serverID, Kind: report.KindUser, ReporterID: testUser,
		ReportedUserID: &target, Reason: "acoso", Status: report.StatusPending,
	}}

	w := do(newRouter(f), http.MethodPost, basePath(serverID), testUser, `{"kind":"user","user_id":"user-2","reason":"acoso"}`)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, body %s", w.Code, w.Body.String())
	}
	if f.gotCreate.UserID != "user-2" || f.gotCreate.ChannelID != nil || f.gotCreate.MessageID != nil {
		t.Errorf("params = %+v", f.gotCreate)
	}
}

func TestCreate_RequiresAuthentication(t *testing.T) {
	f := &fakeReports{}
	w := do(newRouter(f), http.MethodPost, basePath(uuid.New()), "", `{"kind":"user","user_id":"u","reason":"x"}`)
	if w.Code != http.StatusUnauthorized || f.calls != 0 {
		t.Fatalf("status = %d, calls = %d", w.Code, f.calls)
	}
}

func TestCreate_BadInputNeverReachesTheService(t *testing.T) {
	cases := map[string]struct {
		path, body string
		field      string
	}{
		"invalid server id":     {"/v1/servers/nope/reports", `{}`, "serverId"},
		"malformed json":        {basePath(uuid.New()), `{"kind":`, ""},
		"invalid channel id":    {basePath(uuid.New()), `{"kind":"message","channel_id":"zzz","reason":"x"}`, "channel_id"},
		"invalid message id":    {basePath(uuid.New()), `{"kind":"message","channel_id":"` + uuid.NewString() + `","message_id":"zzz","reason":"x"}`, "message_id"},
		"non-object body":       {basePath(uuid.New()), `[1,2]`, ""},
		"non-string reason":     {basePath(uuid.New()), `{"kind":"user","user_id":"u","reason":5}`, ""},
		"non-string user id":    {basePath(uuid.New()), `{"kind":"user","user_id":7,"reason":"x"}`, ""},
		"non-string kind":       {basePath(uuid.New()), `{"kind":true}`, ""},
		"non-string message id": {basePath(uuid.New()), `{"kind":"message","message_id":3}`, ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := &fakeReports{}
			w := do(newRouter(f), http.MethodPost, tc.path, testUser, tc.body)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, body %s", w.Code, w.Body.String())
			}
			if f.calls != 0 {
				t.Errorf("the service was called")
			}
			if tc.field != "" {
				if got := decodeError(t, w).Error.Details["field"]; got != tc.field {
					t.Errorf("field = %v, want %s", got, tc.field)
				}
			}
		})
	}
}

func TestCreate_DomainErrors(t *testing.T) {
	cases := map[string]struct {
		err    error
		status int
		reason string
	}{
		"duplicate (CA2)":    {model.ErrReportAlreadyPending, http.StatusConflict, "report_already_pending"},
		"missing reason":     {model.ErrReportReasonRequired, http.StatusBadRequest, "reason_required"},
		"bad kind":           {model.ErrReportKindInvalid, http.StatusBadRequest, "kind_invalid"},
		"reason too long":    {moderation.ErrReasonTooLong, http.StatusBadRequest, "reason_too_long"},
		"missing channel":    {model.ErrReportChannelRequired, http.StatusBadRequest, "required"},
		"missing message":    {model.ErrReportMessageRequired, http.StatusBadRequest, "required"},
		"missing user":       {model.ErrReportUserRequired, http.StatusBadRequest, "required"},
		"not a text channel": {model.ErrReportTextChannel, http.StatusBadRequest, "text_channel_required"},
		"report yourself":    {model.ErrCannotReportSelf, http.StatusBadRequest, "cannot_report_self"},
		"not a member":       {model.ErrForbiddenNotMember, http.StatusForbidden, ""},
		"unknown channel":    {model.ErrChannelNotFound, http.StatusNotFound, ""},
		"unknown member":     {model.ErrMemberNotFound, http.StatusNotFound, ""},
		"unknown server":     {model.ErrServerNotFound, http.StatusNotFound, ""},
		"no identity":        {model.ErrUnauthorized, http.StatusUnauthorized, ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := &fakeReports{err: tc.err}
			w := do(newRouter(f), http.MethodPost, basePath(uuid.New()), testUser, `{"kind":"user","user_id":"u","reason":"x"}`)
			if w.Code != tc.status {
				t.Fatalf("status = %d, want %d, body %s", w.Code, tc.status, w.Body.String())
			}
			if tc.reason != "" {
				if got := decodeError(t, w).Error.Details["reason"]; got != tc.reason {
					t.Errorf("reason = %v, want %s", got, tc.reason)
				}
			}
		})
	}
}

func TestList_ReturnsThePageAndPassesTheFilters(t *testing.T) {
	serverID := uuid.New()
	target := "user-2"
	f := &fakeReports{page: report.Page{
		Reports: []report.Report{{
			ID: uuid.New(), ServerID: serverID, Kind: report.KindUser, ReporterID: "user-3",
			ReportedUserID: &target, Reason: "acoso", Status: report.StatusPending,
		}},
		Total: 9, Limit: 5, Offset: 10,
	}}

	w := do(newRouter(f), http.MethodGet, basePath(serverID)+"?status=resolved&limit=5&offset=10", testUser, "")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", w.Code, w.Body.String())
	}
	var body report.ListResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("bad body: %v", err)
	}
	if body.Total != 9 || body.Limit != 5 || body.Offset != 10 || len(body.Reports) != 1 || body.Reports[0].ReportedUserID == nil {
		t.Errorf("body = %+v", body)
	}
	got := f.gotList
	if got.ServerID != serverID || got.ActorUserID != testUser || got.Status != "resolved" || got.Limit != 5 || got.Offset != 10 {
		t.Errorf("params = %+v", got)
	}
}

func TestList_AnEmptyPageIsAnEmptyArray(t *testing.T) {
	f := &fakeReports{}
	w := do(newRouter(f), http.MethodGet, basePath(uuid.New()), testUser, "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var raw map[string]json.RawMessage
	_ = json.Unmarshal(w.Body.Bytes(), &raw)
	if string(raw["reports"]) != "[]" {
		t.Errorf("reports = %s, want []", raw["reports"])
	}
	if f.gotList.Status != "" {
		t.Errorf("status = %q, want it left to the service default", f.gotList.Status)
	}
}

func TestList_RejectsBadQueryAndForbiddenCallers(t *testing.T) {
	for _, q := range []string{"?limit=-1", "?limit=abc", "?offset=-3", "?offset=x"} {
		f := &fakeReports{}
		w := do(newRouter(f), http.MethodGet, basePath(uuid.New())+q, testUser, "")
		if w.Code != http.StatusBadRequest || f.calls != 0 {
			t.Errorf("%s: status = %d, calls = %d", q, w.Code, f.calls)
		}
	}

	for name, tc := range map[string]struct {
		err    error
		status int
	}{
		"not a moderator": {model.ErrForbiddenMissingPermission, http.StatusForbidden},
		"not a member":    {model.ErrForbiddenNotMember, http.StatusForbidden},
		"bad status":      {model.ErrReportStatusInvalid, http.StatusBadRequest},
		"no server":       {model.ErrServerNotFound, http.StatusNotFound},
	} {
		f := &fakeReports{err: tc.err}
		w := do(newRouter(f), http.MethodGet, basePath(uuid.New()), testUser, "")
		if w.Code != tc.status {
			t.Errorf("%s: status = %d, want %d", name, w.Code, tc.status)
		}
	}

	w := do(newRouter(&fakeReports{}), http.MethodGet, basePath(uuid.New()), "", "")
	if w.Code != http.StatusUnauthorized {
		t.Errorf("anonymous: status = %d", w.Code)
	}
}

func TestClose_Returns200AndPassesTheStatus(t *testing.T) {
	serverID, reportID := uuid.New(), uuid.New()
	by := testUser
	at := time.Date(2026, 11, 4, 13, 0, 0, 0, time.UTC)
	f := &fakeReports{closed: report.Report{
		ID: reportID, ServerID: serverID, Kind: report.KindUser, Status: report.StatusDismissed,
		ResolvedBy: &by, ResolvedAt: &at,
	}}

	w := do(newRouter(f), http.MethodPatch, basePath(serverID)+"/"+reportID.String(), testUser, `{"status":"dismissed"}`)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", w.Code, w.Body.String())
	}
	var body report.Response
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body.Status != "dismissed" || body.ResolvedBy == nil || *body.ResolvedBy != testUser || body.ResolvedAt == nil {
		t.Errorf("body = %+v", body)
	}
	got := f.gotClose
	if got.ServerID != serverID || got.ReportID != reportID || got.ActorUserID != testUser || got.Status != "dismissed" {
		t.Errorf("params = %+v", got)
	}
}

func TestClose_Errors(t *testing.T) {
	serverID, reportID := uuid.New(), uuid.New()
	path := basePath(serverID) + "/" + reportID.String()

	for name, tc := range map[string]struct {
		err    error
		status int
		reason string
	}{
		"already closed": {model.ErrReportNotPending, http.StatusConflict, "report_not_pending"},
		"unknown report": {model.ErrReportNotFound, http.StatusNotFound, "report_not_found"},
		"not a moderator": {
			model.ErrForbiddenMissingPermission, http.StatusForbidden, "missing_permission",
		},
		"bad status": {model.ErrReportStatusInvalid, http.StatusBadRequest, ""},
	} {
		f := &fakeReports{err: tc.err}
		w := do(newRouter(f), http.MethodPatch, path, testUser, `{"status":"resolved"}`)
		if w.Code != tc.status {
			t.Errorf("%s: status = %d, want %d", name, w.Code, tc.status)
			continue
		}
		if tc.reason != "" && decodeError(t, w).Error.Details["reason"] != tc.reason {
			t.Errorf("%s: details = %v", name, decodeError(t, w).Error.Details)
		}
	}

	for name, tc := range map[string]struct{ path, body string }{
		"invalid report id": {basePath(serverID) + "/nope", `{"status":"resolved"}`},
		"invalid server id": {"/v1/servers/nope/reports/" + reportID.String(), `{"status":"resolved"}`},
		"malformed json":    {path, `{"status":`},
		"status not text":   {path, `{"status":1}`},
	} {
		f := &fakeReports{}
		w := do(newRouter(f), http.MethodPatch, tc.path, testUser, tc.body)
		if w.Code != http.StatusBadRequest || f.calls != 0 {
			t.Errorf("%s: status = %d, calls = %d", name, w.Code, f.calls)
		}
	}

	if w := do(newRouter(&fakeReports{}), http.MethodPatch, path, "", `{"status":"resolved"}`); w.Code != http.StatusUnauthorized {
		t.Errorf("anonymous: status = %d", w.Code)
	}
}
