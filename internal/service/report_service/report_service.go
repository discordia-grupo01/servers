package report_service

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/model/report"
	"github.com/discordia-grupo01/servers/internal/model/role"
	"github.com/discordia-grupo01/servers/internal/service"
)

type Repository interface {
	Create(ctx context.Context, rep report.Report) error
	List(ctx context.Context, serverID uuid.UUID, status *report.Status, limit, offset int) ([]report.Report, int, error)
	Close(ctx context.Context, serverID, reportID uuid.UUID, status report.Status, action *report.ResolutionAction, by string, at time.Time) (report.Report, error)
}

type Service struct {
	servers    service.ServerGetter
	standings  service.StandingReader
	visibility service.ChannelVisibility
	repo       Repository
	now        func() time.Time
}

func New(servers service.ServerGetter, standings service.StandingReader, visibility service.ChannelVisibility, repo Repository) *Service {
	return &Service{
		servers:    servers,
		standings:  standings,
		visibility: visibility,
		repo:       repo,
		now:        func() time.Time { return time.Now().UTC() },
	}
}

type CreateParams struct {
	ServerID    uuid.UUID
	ActorUserID string
	Kind        string
	ChannelID   *uuid.UUID
	MessageID   *uuid.UUID
	UserID      string
	Reason      string
}

type ListParams struct {
	ServerID    uuid.UUID
	ActorUserID string
	Status      string
	Limit       int
	Offset      int
}

type CloseParams struct {
	ServerID    uuid.UUID
	ReportID    uuid.UUID
	ActorUserID string
	Status      string
	// What the moderator did, only when resolving. Empty means none.
	Action string
}

func (s *Service) Create(ctx context.Context, p CreateParams) (report.Report, error) {
	if p.ActorUserID == "" {
		return report.Report{}, model.ErrUnauthorized
	}
	kind, ok := report.ParseKind(p.Kind)
	if !ok {
		return report.Report{}, model.ErrReportKindInvalid
	}
	reason, err := report.NormalizeReason(p.Reason)
	if err != nil {
		return report.Report{}, err
	}
	target := strings.TrimSpace(p.UserID)
	if err := requireTarget(kind, p, target); err != nil {
		return report.Report{}, err
	}

	srv, err := s.servers.GetByID(ctx, p.ServerID)
	if err != nil {
		return report.Report{}, err
	}
	actor, err := service.ResolveStanding(ctx, s.standings, srv, p.ActorUserID)
	if err != nil {
		return report.Report{}, err
	}
	if !actor.IsMember {
		return report.Report{}, model.ErrForbiddenNotMember
	}

	rep := report.Report{
		ID:         uuid.New(),
		ServerID:   p.ServerID,
		Kind:       kind,
		ReporterID: p.ActorUserID,
		Reason:     reason,
		Status:     report.StatusPending,
		CreatedAt:  s.now(),
	}
	switch kind {
	case report.KindMessage:
		if err := s.checkChannel(ctx, srv, actor, p.ActorUserID, *p.ChannelID); err != nil {
			return report.Report{}, err
		}
		rep.ChannelID = p.ChannelID
		rep.MessageID = p.MessageID
	case report.KindUser:
		if err := s.checkUser(ctx, srv, p.ActorUserID, target); err != nil {
			return report.Report{}, err
		}
		rep.ReportedUserID = &target
	}

	if err := s.repo.Create(ctx, rep); err != nil {
		return report.Report{}, err
	}
	return rep, nil
}

func (s *Service) List(ctx context.Context, p ListParams) (report.Page, error) {
	if _, err := s.authorizeModerator(ctx, p.ServerID, p.ActorUserID); err != nil {
		return report.Page{}, err
	}
	status, err := listFilter(p.Status)
	if err != nil {
		return report.Page{}, err
	}
	limit, offset := report.ClampPage(p.Limit, p.Offset)
	reports, total, err := s.repo.List(ctx, p.ServerID, status, limit, offset)
	if err != nil {
		return report.Page{}, err
	}
	return report.Page{Reports: reports, Total: total, Limit: limit, Offset: offset}, nil
}

func (s *Service) Close(ctx context.Context, p CloseParams) (report.Report, error) {
	status, ok := report.ParseClosingStatus(p.Status)
	if !ok {
		return report.Report{}, model.ErrReportStatusInvalid
	}
	action, err := closingAction(status, p.Action)
	if err != nil {
		return report.Report{}, err
	}
	if _, err := s.authorizeModerator(ctx, p.ServerID, p.ActorUserID); err != nil {
		return report.Report{}, err
	}
	return s.repo.Close(ctx, p.ServerID, p.ReportID, status, action, p.ActorUserID, s.now())
}

func closingAction(status report.Status, raw string) (*report.ResolutionAction, error) {
	if raw == "" {
		return nil, nil
	}
	if status != report.StatusResolved {
		return nil, model.ErrReportActionInvalid
	}
	action, ok := report.ParseResolutionAction(raw)
	if !ok {
		return nil, model.ErrReportActionInvalid
	}
	return &action, nil
}

func listFilter(raw string) (*report.Status, error) {
	switch raw {
	case "":
		pending := report.StatusPending
		return &pending, nil
	case "all":
		return nil, nil
	}
	parsed, ok := report.ParseStatus(raw)
	if !ok {
		return nil, model.ErrReportStatusInvalid
	}
	return &parsed, nil
}

func requireTarget(kind report.Kind, p CreateParams, user string) error {
	switch kind {
	case report.KindMessage:
		if p.ChannelID == nil {
			return model.ErrReportChannelRequired
		}
		if p.MessageID == nil {
			return model.ErrReportMessageRequired
		}
	case report.KindUser:
		if user == "" {
			return model.ErrReportUserRequired
		}
	}
	return nil
}

func (s *Service) checkChannel(ctx context.Context, srv model.Server, actor role.Standing, actorID string, channelID uuid.UUID) error {
	var channel *model.Channel
	for i := range srv.Channels {
		if srv.Channels[i].ID == channelID {
			channel = &srv.Channels[i]
			break
		}
	}
	if channel == nil {
		return model.ErrChannelNotFound
	}
	if err := service.RequireVisibleChannel(ctx, s.visibility, srv, actor, actorID, channelID); err != nil {
		return err
	}
	if channel.Kind != model.ChannelText {
		return model.ErrReportTextChannel
	}
	return nil
}

func (s *Service) checkUser(ctx context.Context, srv model.Server, actorID, target string) error {
	if target == actorID {
		return model.ErrCannotReportSelf
	}
	standing, err := service.ResolveStanding(ctx, s.standings, srv, target)
	if err != nil {
		return err
	}
	if !standing.IsMember {
		return model.ErrMemberNotFound
	}
	return nil
}

func (s *Service) authorizeModerator(ctx context.Context, serverID uuid.UUID, actorID string) (role.Standing, error) {
	if actorID == "" {
		return role.Standing{}, model.ErrUnauthorized
	}
	srv, err := s.servers.GetByID(ctx, serverID)
	if err != nil {
		return role.Standing{}, err
	}
	actor, err := service.ResolveStanding(ctx, s.standings, srv, actorID)
	if err != nil {
		return role.Standing{}, err
	}
	if !actor.IsMember {
		return role.Standing{}, model.ErrForbiddenNotMember
	}
	if err := service.RequireModerator(actor); err != nil {
		return role.Standing{}, err
	}
	return actor, nil
}
