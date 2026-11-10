package report

import (
	"time"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/model/moderation"
	"github.com/discordia-grupo01/servers/internal/model/userprofile"
)

type Kind string

const (
	KindMessage Kind = "message"
	KindUser    Kind = "user"
)

type Status string

const (
	StatusPending   Status = "pending"
	StatusResolved  Status = "resolved"
	StatusDismissed Status = "dismissed"
)

type ResolutionAction string

const (
	ActionDeleteMessage ResolutionAction = "delete_message"
	ActionMute          ResolutionAction = "mute"
	ActionKick          ResolutionAction = "kick"
	ActionBan           ResolutionAction = "ban"
)

func ParseResolutionAction(raw string) (ResolutionAction, bool) {
	switch a := ResolutionAction(raw); a {
	case ActionDeleteMessage, ActionMute, ActionKick, ActionBan:
		return a, true
	}
	return "", false
}

func ParseKind(raw string) (Kind, bool) {
	switch k := Kind(raw); k {
	case KindMessage, KindUser:
		return k, true
	}
	return "", false
}

func ParseStatus(raw string) (Status, bool) {
	switch s := Status(raw); s {
	case StatusPending, StatusResolved, StatusDismissed:
		return s, true
	}
	return "", false
}

func ParseClosingStatus(raw string) (Status, bool) {
	s, ok := ParseStatus(raw)
	return s, ok && s != StatusPending
}

type Report struct {
	ID               uuid.UUID
	ServerID         uuid.UUID
	Kind             Kind
	ReporterID       string
	ChannelID        *uuid.UUID
	MessageID        *uuid.UUID
	ReportedUserID   *string
	Reason           string
	Status           Status
	CreatedAt        time.Time
	ResolvedAt       *time.Time
	ResolvedBy       *string
	ResolutionAction *ResolutionAction

	ReporterProfile *userprofile.Profile
	ReportedProfile *userprofile.Profile
}

func NormalizeReason(raw string) (string, error) {
	reason, err := moderation.NormalizeReason(&raw)
	if err != nil {
		return "", err
	}
	if reason == nil {
		return "", model.ErrReportReasonRequired
	}
	return *reason, nil
}

type Page struct {
	Reports []Report
	Total   int
	Limit   int
	Offset  int
}

func ClampPage(limit, offset int) (int, int) {
	switch {
	case limit <= 0:
		limit = moderation.ListDefaultLimit
	case limit > moderation.ListMaxLimit:
		limit = moderation.ListMaxLimit
	}
	return limit, max(offset, 0)
}

type Response struct {
	ID             string     `json:"id"`
	ServerID       string     `json:"server_id"`
	Kind           string     `json:"kind"`
	ReporterID     string     `json:"reporter_id"`
	ChannelID      *string    `json:"channel_id"`
	MessageID      *string    `json:"message_id"`
	ReportedUserID *string    `json:"reported_user_id"`
	Reason         string     `json:"reason"`
	Status         string     `json:"status"`
	CreatedAt      time.Time  `json:"created_at"`
	ResolvedAt     *time.Time `json:"resolved_at"`
	ResolvedBy     *string    `json:"resolved_by"`
	// delete_message, mute, kick or ban; null if it was closed without one.
	ResolutionAction *string                      `json:"resolution_action" enums:"delete_message,mute,kick,ban"`
	ReporterProfile  *model.MemberProfileResponse `json:"reporter_profile"`
	ReportedProfile  *model.MemberProfileResponse `json:"reported_profile"`
}

type ListResponse struct {
	Reports []Response `json:"reports"`
	Total   int        `json:"total"`
	Limit   int        `json:"limit"`
	Offset  int        `json:"offset"`
}

func ToResponse(r Report) Response {
	return Response{
		ID:               r.ID.String(),
		ServerID:         r.ServerID.String(),
		Kind:             string(r.Kind),
		ReporterID:       r.ReporterID,
		ChannelID:        uuidString(r.ChannelID),
		MessageID:        uuidString(r.MessageID),
		ReportedUserID:   r.ReportedUserID,
		Reason:           r.Reason,
		Status:           string(r.Status),
		CreatedAt:        r.CreatedAt,
		ResolvedAt:       r.ResolvedAt,
		ResolvedBy:       r.ResolvedBy,
		ResolutionAction: actionString(r.ResolutionAction),
		ReporterProfile:  model.NewMemberProfileResponse(r.ReporterProfile),
		ReportedProfile:  model.NewMemberProfileResponse(r.ReportedProfile),
	}
}

func ToListResponse(p Page) ListResponse {
	reports := make([]Response, 0, len(p.Reports))
	for _, r := range p.Reports {
		reports = append(reports, ToResponse(r))
	}
	return ListResponse{Reports: reports, Total: p.Total, Limit: p.Limit, Offset: p.Offset}
}

func actionString(a *ResolutionAction) *string {
	if a == nil {
		return nil
	}
	s := string(*a)
	return &s
}

func uuidString(id *uuid.UUID) *string {
	if id == nil {
		return nil
	}
	s := id.String()
	return &s
}
