package report_service_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/model/channelperm"
	"github.com/discordia-grupo01/servers/internal/model/moderation"
	"github.com/discordia-grupo01/servers/internal/model/report"
	"github.com/discordia-grupo01/servers/internal/model/role"
	"github.com/discordia-grupo01/servers/internal/service/report_service"
)

const (
	ownerID      = "owner-1"
	moderatorID  = "mod-1"
	memberID     = "member-1"
	otherID      = "member-2"
	outsiderID   = "outsider-1"
	adminID      = "admin-1"
	messagesOnly = "mod-messages"
)

type fakeServers struct{ server model.Server }

func (f *fakeServers) GetByID(context.Context, uuid.UUID) (model.Server, error) {
	return f.server, nil
}

type fakeStandings struct{ byUser map[string]role.Standing }

func (f *fakeStandings) MemberStanding(_ context.Context, _ uuid.UUID, userID string) (role.Standing, error) {
	return f.byUser[userID], nil
}

type fakeVisibility struct{ hiddenChannel uuid.UUID }

func (f *fakeVisibility) MemberStandings(context.Context, string, []uuid.UUID) (map[uuid.UUID]role.Standing, error) {
	return nil, nil
}

func (f *fakeVisibility) ChannelLayers(context.Context, string, []uuid.UUID) (map[uuid.UUID]channelperm.Layers, error) {
	return map[uuid.UUID]channelperm.Layers{
		f.hiddenChannel: {Everyone: channelperm.Override{Deny: role.PermViewChannels}},
	}, nil
}

type closeCall struct {
	serverID, reportID uuid.UUID
	status             report.Status
	action             string
	by                 string
}

type fakeRepo struct {
	created     []report.Report
	createErr   error
	listed      []report.Report
	listTotal   int
	listStatus  **report.Status
	listLimit   int
	listOffset  int
	listCalls   int
	closeCalls  []closeCall
	closeResult report.Report
	closeErr    error
}

func (f *fakeRepo) Create(_ context.Context, rep report.Report) error {
	if f.createErr != nil {
		return f.createErr
	}
	f.created = append(f.created, rep)
	return nil
}

func (f *fakeRepo) List(_ context.Context, _ uuid.UUID, status *report.Status, limit, offset int) ([]report.Report, int, error) {
	f.listCalls++
	f.listStatus = &status
	f.listLimit, f.listOffset = limit, offset
	return f.listed, f.listTotal, nil
}

func (f *fakeRepo) Close(_ context.Context, serverID, reportID uuid.UUID, status report.Status, action *report.ResolutionAction, by string, _ time.Time) (report.Report, error) {
	var a string
	if action != nil {
		a = string(*action)
	}
	f.closeCalls = append(f.closeCalls, closeCall{serverID, reportID, status, a, by})
	return f.closeResult, f.closeErr
}

type fixture struct {
	svc       *report_service.Service
	repo      *fakeRepo
	serverID  uuid.UUID
	text      uuid.UUID
	voice     uuid.UUID
	hidden    uuid.UUID
	foreignCh uuid.UUID
}

func newFixture() *fixture {
	f := &fixture{
		serverID:  uuid.New(),
		text:      uuid.New(),
		voice:     uuid.New(),
		hidden:    uuid.New(),
		foreignCh: uuid.New(),
		repo:      &fakeRepo{},
	}
	server := model.Server{
		ID:      f.serverID,
		OwnerID: ownerID,
		Channels: []model.Channel{
			{ID: f.text, ServerID: f.serverID, Name: "general", Kind: model.ChannelText},
			{ID: f.voice, ServerID: f.serverID, Name: "voz", Kind: model.ChannelVoice},
			{ID: f.hidden, ServerID: f.serverID, Name: "staff", Kind: model.ChannelText},
		},
	}
	standings := &fakeStandings{byUser: map[string]role.Standing{
		moderatorID:  {IsMember: true, Permissions: role.DefaultPermissions | role.ModeratorPermissions},
		messagesOnly: {IsMember: true, Permissions: role.DefaultPermissions | role.PermManageMessages},
		memberID:     {IsMember: true, Permissions: role.DefaultPermissions},
		otherID:      {IsMember: true, Permissions: role.DefaultPermissions},
		adminID:      {IsMember: true, Permissions: role.PermAdministrator},
	}}
	f.svc = report_service.New(&fakeServers{server: server}, standings, &fakeVisibility{hiddenChannel: f.hidden}, f.repo)
	return f
}

func (f *fixture) messageReport(actor string, channel uuid.UUID, reason string) report_service.CreateParams {
	message := uuid.New()
	return report_service.CreateParams{
		ServerID: f.serverID, ActorUserID: actor, Kind: "message", ChannelID: &channel, MessageID: &message, Reason: reason,
	}
}

func (f *fixture) userReport(actor, target, reason string) report_service.CreateParams {
	return report_service.CreateParams{
		ServerID: f.serverID, ActorUserID: actor, Kind: "user", UserID: target, Reason: reason,
	}
}

func TestCreate_CA1_AMemberReportsAMessage(t *testing.T) {
	f := newFixture()
	params := f.messageReport(memberID, f.text, "  Insultos  ")

	got, err := f.svc.Create(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(f.repo.created) != 1 {
		t.Fatalf("created = %d reports, want 1", len(f.repo.created))
	}
	stored := f.repo.created[0]
	if stored.ID == uuid.Nil || stored.ID != got.ID {
		t.Errorf("id = %v, returned %v", stored.ID, got.ID)
	}
	if stored.Kind != report.KindMessage || stored.Status != report.StatusPending {
		t.Errorf("kind/status = %q/%q", stored.Kind, stored.Status)
	}
	if stored.ServerID != f.serverID || stored.ReporterID != memberID || stored.Reason != "Insultos" {
		t.Errorf("stored = %+v", stored)
	}
	if stored.ChannelID == nil || *stored.ChannelID != f.text || stored.MessageID == nil || *stored.MessageID != *params.MessageID {
		t.Errorf("target = %v / %v", stored.ChannelID, stored.MessageID)
	}
	if stored.ReportedUserID != nil || stored.ResolvedAt != nil || stored.ResolvedBy != nil || stored.CreatedAt.IsZero() {
		t.Errorf("stored = %+v", stored)
	}
}

func TestCreate_CA1_AMemberReportsAUser(t *testing.T) {
	f := newFixture()

	got, err := f.svc.Create(context.Background(), f.userReport(memberID, " "+otherID+" ", "Acoso"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Kind != report.KindUser || got.ReportedUserID == nil || *got.ReportedUserID != otherID {
		t.Errorf("got = %+v", got)
	}
	if got.ChannelID != nil || got.MessageID != nil {
		t.Errorf("a user report must not carry a message: %+v", got)
	}
	if len(f.repo.created) != 1 {
		t.Fatalf("created = %d reports", len(f.repo.created))
	}
}

func TestCreate_EveryKindOfMemberCanReport(t *testing.T) {
	for _, actor := range []string{ownerID, adminID, moderatorID, messagesOnly, memberID} {
		f := newFixture()
		if _, err := f.svc.Create(context.Background(), f.messageReport(actor, f.text, "x")); err != nil {
			t.Errorf("%s: unexpected error %v", actor, err)
		}
	}
}

func TestCreate_AUserCanBeReportedByAnyMemberEvenTheOwner(t *testing.T) {
	f := newFixture()
	if _, err := f.svc.Create(context.Background(), f.userReport(memberID, ownerID, "abuso")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCreate_CA2_APendingDuplicateIsReportedAsSuch(t *testing.T) {
	f := newFixture()
	f.repo.createErr = model.ErrReportAlreadyPending

	_, err := f.svc.Create(context.Background(), f.messageReport(memberID, f.text, "x"))
	if !errors.Is(err, model.ErrReportAlreadyPending) {
		t.Fatalf("err = %v, want ErrReportAlreadyPending", err)
	}
	_, err = f.svc.Create(context.Background(), f.userReport(memberID, otherID, "x"))
	if !errors.Is(err, model.ErrReportAlreadyPending) {
		t.Fatalf("err = %v, want ErrReportAlreadyPending", err)
	}
}

func TestCreate_Rejections(t *testing.T) {
	cases := map[string]struct {
		params func(f *fixture) report_service.CreateParams
		want   error
	}{
		"no actor": {
			params: func(f *fixture) report_service.CreateParams { return f.messageReport("", f.text, "x") },
			want:   model.ErrUnauthorized,
		},
		"unknown kind": {
			params: func(f *fixture) report_service.CreateParams {
				p := f.messageReport(memberID, f.text, "x")
				p.Kind = "channel"
				return p
			},
			want: model.ErrReportKindInvalid,
		},
		"missing reason": {
			params: func(f *fixture) report_service.CreateParams { return f.messageReport(memberID, f.text, "") },
			want:   model.ErrReportReasonRequired,
		},
		"blank reason": {
			params: func(f *fixture) report_service.CreateParams { return f.userReport(memberID, otherID, "  \n ") },
			want:   model.ErrReportReasonRequired,
		},
		"reason too long": {
			params: func(f *fixture) report_service.CreateParams {
				return f.userReport(memberID, otherID, strings.Repeat("a", moderation.ReasonMaxLen+1))
			},
			want: moderation.ErrReasonTooLong,
		},
		"reason with control characters": {
			params: func(f *fixture) report_service.CreateParams { return f.userReport(memberID, otherID, "mal\x00") },
			want:   moderation.ErrReasonInvalidChars,
		},
		"message report without channel": {
			params: func(f *fixture) report_service.CreateParams {
				p := f.messageReport(memberID, f.text, "x")
				p.ChannelID = nil
				return p
			},
			want: model.ErrReportChannelRequired,
		},
		"message report without message": {
			params: func(f *fixture) report_service.CreateParams {
				p := f.messageReport(memberID, f.text, "x")
				p.MessageID = nil
				return p
			},
			want: model.ErrReportMessageRequired,
		},
		"user report without user": {
			params: func(f *fixture) report_service.CreateParams { return f.userReport(memberID, "  ", "x") },
			want:   model.ErrReportUserRequired,
		},
		"reporter is not a member": {
			params: func(f *fixture) report_service.CreateParams { return f.messageReport(outsiderID, f.text, "x") },
			want:   model.ErrForbiddenNotMember,
		},
		"channel of another server": {
			params: func(f *fixture) report_service.CreateParams { return f.messageReport(memberID, f.foreignCh, "x") },
			want:   model.ErrChannelNotFound,
		},
		"channel the reporter cannot see": {
			params: func(f *fixture) report_service.CreateParams { return f.messageReport(memberID, f.hidden, "x") },
			want:   model.ErrChannelNotFound,
		},
		"voice channel": {
			params: func(f *fixture) report_service.CreateParams { return f.messageReport(memberID, f.voice, "x") },
			want:   model.ErrReportTextChannel,
		},
		"reporting yourself": {
			params: func(f *fixture) report_service.CreateParams { return f.userReport(memberID, memberID, "x") },
			want:   model.ErrCannotReportSelf,
		},
		"reported user is not a member": {
			params: func(f *fixture) report_service.CreateParams { return f.userReport(memberID, outsiderID, "x") },
			want:   model.ErrMemberNotFound,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture()
			_, err := f.svc.Create(context.Background(), tc.params(f))
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if len(f.repo.created) != 0 {
				t.Errorf("a rejected report was stored: %+v", f.repo.created)
			}
		})
	}
}

func TestCreate_TheHiddenChannelIsVisibleToWhoCanSeeIt(t *testing.T) {
	f := newFixture()
	for _, actor := range []string{ownerID, adminID} {
		if _, err := f.svc.Create(context.Background(), f.messageReport(actor, f.hidden, "x")); err != nil {
			t.Errorf("%s: unexpected error %v", actor, err)
		}
	}
}

func TestList_OnlyModeratorsSeeTheReports(t *testing.T) {
	for _, actor := range []string{ownerID, adminID, moderatorID} {
		f := newFixture()
		if _, err := f.svc.List(context.Background(), report_service.ListParams{ServerID: f.serverID, ActorUserID: actor}); err != nil {
			t.Errorf("%s: unexpected error %v", actor, err)
		}
	}

	for _, actor := range []string{messagesOnly, memberID} {
		f := newFixture()
		_, err := f.svc.List(context.Background(), report_service.ListParams{ServerID: f.serverID, ActorUserID: actor})
		if !errors.Is(err, model.ErrForbiddenMissingPermission) {
			t.Errorf("%s: err = %v, want ErrForbiddenMissingPermission", actor, err)
		}
		if f.repo.listCalls != 0 {
			t.Errorf("%s: the repository was queried", actor)
		}
	}

	f := newFixture()
	if _, err := f.svc.List(context.Background(), report_service.ListParams{ServerID: f.serverID, ActorUserID: outsiderID}); !errors.Is(err, model.ErrForbiddenNotMember) {
		t.Errorf("outsider: err = %v, want ErrForbiddenNotMember", err)
	}
	if _, err := f.svc.List(context.Background(), report_service.ListParams{ServerID: f.serverID}); !errors.Is(err, model.ErrUnauthorized) {
		t.Errorf("anonymous: err = %v, want ErrUnauthorized", err)
	}
}

func TestList_StatusFilter(t *testing.T) {
	cases := map[string]struct {
		status string
		want   *report.Status
	}{
		"default is the pending reports": {"", ptr(report.StatusPending)},
		"all removes the filter":         {"all", nil},
		"resolved":                       {"resolved", ptr(report.StatusResolved)},
		"dismissed":                      {"dismissed", ptr(report.StatusDismissed)},
		"pending":                        {"pending", ptr(report.StatusPending)},
	}
	for name, tc := range cases {
		f := newFixture()
		if _, err := f.svc.List(context.Background(), report_service.ListParams{ServerID: f.serverID, ActorUserID: moderatorID, Status: tc.status}); err != nil {
			t.Fatalf("%s: unexpected error %v", name, err)
		}
		got := *f.repo.listStatus
		switch {
		case tc.want == nil && got != nil:
			t.Errorf("%s: filter = %v, want none", name, *got)
		case tc.want != nil && (got == nil || *got != *tc.want):
			t.Errorf("%s: filter = %v, want %v", name, got, *tc.want)
		}
	}

	f := newFixture()
	_, err := f.svc.List(context.Background(), report_service.ListParams{ServerID: f.serverID, ActorUserID: moderatorID, Status: "bogus"})
	if !errors.Is(err, model.ErrReportStatusInvalid) {
		t.Errorf("bogus status: err = %v, want ErrReportStatusInvalid", err)
	}
}

func TestList_ReturnsThePageWithItsClampedLimits(t *testing.T) {
	f := newFixture()
	f.repo.listed = []report.Report{{ID: uuid.New()}, {ID: uuid.New()}}
	f.repo.listTotal = 7

	page, err := f.svc.List(context.Background(), report_service.ListParams{
		ServerID: f.serverID, ActorUserID: moderatorID, Limit: 5000, Offset: -2,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if page.Total != 7 || len(page.Reports) != 2 {
		t.Errorf("page = %+v", page)
	}
	if page.Limit != moderation.ListMaxLimit || page.Offset != 0 {
		t.Errorf("limit/offset = %d/%d", page.Limit, page.Offset)
	}
	if f.repo.listLimit != moderation.ListMaxLimit || f.repo.listOffset != 0 {
		t.Errorf("repository got %d/%d", f.repo.listLimit, f.repo.listOffset)
	}
}

func TestClose_AModeratorClosesAPendingReport(t *testing.T) {
	for _, status := range []string{"resolved", "dismissed"} {
		f := newFixture()
		reportID := uuid.New()
		f.repo.closeResult = report.Report{ID: reportID, Status: report.Status(status)}

		got, err := f.svc.Close(context.Background(), report_service.CloseParams{
			ServerID: f.serverID, ReportID: reportID, ActorUserID: moderatorID, Status: status,
		})
		if err != nil {
			t.Fatalf("%s: unexpected error %v", status, err)
		}
		if got.ID != reportID || got.Status != report.Status(status) {
			t.Errorf("%s: got = %+v", status, got)
		}
		want := closeCall{f.serverID, reportID, report.Status(status), "", moderatorID}
		if len(f.repo.closeCalls) != 1 || f.repo.closeCalls[0] != want {
			t.Errorf("%s: calls = %+v, want %+v", status, f.repo.closeCalls, want)
		}
	}
}

func TestClose_Rejections(t *testing.T) {
	cases := map[string]struct {
		actor  string
		status string
		want   error
	}{
		"pending is not a closing status": {moderatorID, "pending", model.ErrReportStatusInvalid},
		"unknown status":                  {moderatorID, "closed", model.ErrReportStatusInvalid},
		"empty status":                    {moderatorID, "", model.ErrReportStatusInvalid},
		"a regular member":                {memberID, "resolved", model.ErrForbiddenMissingPermission},
		"only MANAGE_MESSAGES":            {messagesOnly, "resolved", model.ErrForbiddenMissingPermission},
		"not a member":                    {outsiderID, "resolved", model.ErrForbiddenNotMember},
		"anonymous":                       {"", "resolved", model.ErrUnauthorized},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture()
			_, err := f.svc.Close(context.Background(), report_service.CloseParams{
				ServerID: f.serverID, ReportID: uuid.New(), ActorUserID: tc.actor, Status: tc.status,
			})
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if len(f.repo.closeCalls) != 0 {
				t.Errorf("the report was closed: %+v", f.repo.closeCalls)
			}
		})
	}
}

func TestClose_ResolvingCarriesTheAction(t *testing.T) {
	for _, action := range []string{"delete_message", "mute", "kick", "ban"} {
		f := newFixture()
		reportID := uuid.New()
		_, err := f.svc.Close(context.Background(), report_service.CloseParams{
			ServerID: f.serverID, ReportID: reportID, ActorUserID: moderatorID, Status: "resolved", Action: action,
		})
		if err != nil {
			t.Fatalf("%s: unexpected error %v", action, err)
		}
		want := closeCall{f.serverID, reportID, report.StatusResolved, action, moderatorID}
		if len(f.repo.closeCalls) != 1 || f.repo.closeCalls[0] != want {
			t.Errorf("%s: calls = %+v, want %+v", action, f.repo.closeCalls, want)
		}
	}
}

func TestClose_ActionRejections(t *testing.T) {
	cases := map[string]struct{ status, action string }{
		"an action when dismissing": {"dismissed", "mute"},
		"an unknown action":         {"resolved", "warn"},
		"an action in another case": {"resolved", "Mute"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture()
			_, err := f.svc.Close(context.Background(), report_service.CloseParams{
				ServerID: f.serverID, ReportID: uuid.New(), ActorUserID: moderatorID, Status: tc.status, Action: tc.action,
			})
			if !errors.Is(err, model.ErrReportActionInvalid) {
				t.Fatalf("err = %v, want ErrReportActionInvalid", err)
			}
			if len(f.repo.closeCalls) != 0 {
				t.Errorf("the report was closed: %+v", f.repo.closeCalls)
			}
		})
	}
}

func TestClose_PassesThroughTheKindMismatch(t *testing.T) {
	f := newFixture()
	f.repo.closeErr = model.ErrReportActionKind
	_, err := f.svc.Close(context.Background(), report_service.CloseParams{
		ServerID: f.serverID, ReportID: uuid.New(), ActorUserID: moderatorID, Status: "resolved", Action: "delete_message",
	})
	if !errors.Is(err, model.ErrReportActionKind) {
		t.Errorf("err = %v, want ErrReportActionKind", err)
	}
}

func TestClose_PassesThroughNotFoundAndAlreadyClosed(t *testing.T) {
	for _, want := range []error{model.ErrReportNotFound, model.ErrReportNotPending} {
		f := newFixture()
		f.repo.closeErr = want
		_, err := f.svc.Close(context.Background(), report_service.CloseParams{
			ServerID: f.serverID, ReportID: uuid.New(), ActorUserID: ownerID, Status: "dismissed",
		})
		if !errors.Is(err, want) {
			t.Errorf("err = %v, want %v", err, want)
		}
	}
}

func ptr[T any](v T) *T { return &v }
