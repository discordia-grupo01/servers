package report_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/model/moderation"
	"github.com/discordia-grupo01/servers/internal/model/report"
	"github.com/discordia-grupo01/servers/internal/model/userprofile"
)

func TestParseKind(t *testing.T) {
	for raw, want := range map[string]report.Kind{"message": report.KindMessage, "user": report.KindUser} {
		got, ok := report.ParseKind(raw)
		if !ok || got != want {
			t.Errorf("ParseKind(%q) = %q, %v", raw, got, ok)
		}
	}
	for _, raw := range []string{"", "Message", "channel", " user"} {
		if _, ok := report.ParseKind(raw); ok {
			t.Errorf("ParseKind(%q) accepted", raw)
		}
	}
}

func TestParseStatus(t *testing.T) {
	for _, raw := range []string{"pending", "resolved", "dismissed"} {
		if got, ok := report.ParseStatus(raw); !ok || string(got) != raw {
			t.Errorf("ParseStatus(%q) = %q, %v", raw, got, ok)
		}
	}
	for _, raw := range []string{"", "all", "closed", "Pending"} {
		if _, ok := report.ParseStatus(raw); ok {
			t.Errorf("ParseStatus(%q) accepted", raw)
		}
	}
}

func TestParseClosingStatus_OnlyResolvedAndDismissed(t *testing.T) {
	for _, raw := range []string{"resolved", "dismissed"} {
		if got, ok := report.ParseClosingStatus(raw); !ok || string(got) != raw {
			t.Errorf("ParseClosingStatus(%q) = %q, %v", raw, got, ok)
		}
	}
	for _, raw := range []string{"pending", "", "all", "closed"} {
		if _, ok := report.ParseClosingStatus(raw); ok {
			t.Errorf("ParseClosingStatus(%q) accepted", raw)
		}
	}
}

func TestParseResolutionAction(t *testing.T) {
	for _, raw := range []string{"delete_message", "mute", "kick", "ban"} {
		if got, ok := report.ParseResolutionAction(raw); !ok || string(got) != raw {
			t.Errorf("%q: got %q, %v", raw, got, ok)
		}
	}
	for _, raw := range []string{"", "warn", "MUTE", "unmute", "unban"} {
		if _, ok := report.ParseResolutionAction(raw); ok {
			t.Errorf("%q should be rejected", raw)
		}
	}
}

func TestNormalizeReason(t *testing.T) {
	got, err := report.NormalizeReason("  Insultos \n repetidos  ")
	if err != nil || got != "Insultos \n repetidos" {
		t.Fatalf("got %q, %v", got, err)
	}

	for _, blank := range []string{"", "   ", "\n\t "} {
		if _, err := report.NormalizeReason(blank); !errors.Is(err, model.ErrReportReasonRequired) {
			t.Errorf("NormalizeReason(%q) err = %v, want ErrReportReasonRequired", blank, err)
		}
	}

	if _, err := report.NormalizeReason(strings.Repeat("a", moderation.ReasonMaxLen+1)); !errors.Is(err, moderation.ErrReasonTooLong) {
		t.Errorf("too long: err = %v", err)
	}
	if _, err := report.NormalizeReason(strings.Repeat("a", moderation.ReasonMaxLen)); err != nil {
		t.Errorf("exactly the limit: err = %v", err)
	}
	if _, err := report.NormalizeReason("mal\x00motivo"); !errors.Is(err, moderation.ErrReasonInvalidChars) {
		t.Errorf("control chars: err = %v", err)
	}
}

func TestClampPage(t *testing.T) {
	cases := []struct{ limit, offset, wantLimit, wantOffset int }{
		{0, 0, moderation.ListDefaultLimit, 0},
		{-5, -3, moderation.ListDefaultLimit, 0},
		{10, 4, 10, 4},
		{moderation.ListMaxLimit + 1, 0, moderation.ListMaxLimit, 0},
	}
	for _, tc := range cases {
		limit, offset := report.ClampPage(tc.limit, tc.offset)
		if limit != tc.wantLimit || offset != tc.wantOffset {
			t.Errorf("ClampPage(%d, %d) = %d, %d, want %d, %d", tc.limit, tc.offset, limit, offset, tc.wantLimit, tc.wantOffset)
		}
	}
}

func TestToResponse_Message(t *testing.T) {
	channelID, messageID := uuid.New(), uuid.New()
	at := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	got := report.ToResponse(report.Report{
		ID: uuid.New(), ServerID: uuid.New(), Kind: report.KindMessage, ReporterID: "u1",
		ChannelID: &channelID, MessageID: &messageID, Reason: "spam", Status: report.StatusPending, CreatedAt: at,
		ReporterProfile: &userprofile.Profile{UserID: "u1", Name: "Ana"},
	})
	if got.Kind != "message" || got.Status != "pending" || got.ReportedUserID != nil {
		t.Errorf("response = %+v", got)
	}
	if got.ChannelID == nil || *got.ChannelID != channelID.String() || got.MessageID == nil || *got.MessageID != messageID.String() {
		t.Errorf("ids = %v, %v", got.ChannelID, got.MessageID)
	}
	if got.ReporterProfile == nil || got.ReporterProfile.Name != "Ana" || got.ReportedProfile != nil {
		t.Errorf("profiles = %+v, %+v", got.ReporterProfile, got.ReportedProfile)
	}
}

func TestToResponse_User(t *testing.T) {
	target := "u2"
	got := report.ToResponse(report.Report{
		ID: uuid.New(), ServerID: uuid.New(), Kind: report.KindUser, ReporterID: "u1",
		ReportedUserID: &target, Reason: "acoso", Status: report.StatusResolved,
	})
	if got.ChannelID != nil || got.MessageID != nil || got.ReportedUserID == nil || *got.ReportedUserID != "u2" {
		t.Errorf("response = %+v", got)
	}
}

func TestToResponse_ResolutionAction(t *testing.T) {
	action := report.ActionBan
	got := report.ToResponse(report.Report{ID: uuid.New(), Status: report.StatusResolved, ResolutionAction: &action})
	if got.ResolutionAction == nil || *got.ResolutionAction != "ban" {
		t.Errorf("resolution_action = %v", got.ResolutionAction)
	}
	if none := report.ToResponse(report.Report{ID: uuid.New()}); none.ResolutionAction != nil {
		t.Errorf("resolution_action = %v, want nil", *none.ResolutionAction)
	}
}

func TestToListResponse_NeverNilReports(t *testing.T) {
	got := report.ToListResponse(report.Page{Total: 0, Limit: 20})
	if got.Reports == nil || len(got.Reports) != 0 {
		t.Fatalf("reports = %#v, want an empty non-nil slice", got.Reports)
	}
}
