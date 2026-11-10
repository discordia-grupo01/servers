package report_repo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/model/report"
	"github.com/discordia-grupo01/servers/internal/model/userprofile"
	"github.com/discordia-grupo01/servers/internal/outbox"
)

const (
	pgUniqueViolation = "23505"
	pgCheckViolation  = "23514"

	actionKindConstraint = "reports_resolution_action_kind_check"
)

var pendingConstraints = map[string]bool{
	"uq_reports_pending_message": true,
	"uq_reports_pending_user":    true,
}

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

// Create stores the report as pending and writes servers.report_updated to the
// outbox in the same transaction: there is never a report admin doesn't hear
// about, nor an event for a report that wasn't stored. A duplicate pending
// report fails the INSERT, which rolls everything back, so it publishes nothing.
func (r *Repository) Create(ctx context.Context, rep report.Report) error {
	event, err := UpdatedEvent(rep)
	if err != nil {
		return err
	}
	err = pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			INSERT INTO reports (id, server_id, kind, reporter_id, channel_id, message_id, reported_user_id, reason, status, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
			rep.ID, rep.ServerID, string(rep.Kind), rep.ReporterID, rep.ChannelID, rep.MessageID,
			rep.ReportedUserID, rep.Reason, string(rep.Status), rep.CreatedAt,
		); err != nil {
			return err
		}
		return outbox.Insert(ctx, tx, event)
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation && pendingConstraints[pgErr.ConstraintName] {
			return model.ErrReportAlreadyPending
		}
		return fmt.Errorf("insert report: %w", err)
	}
	return nil
}

func (r *Repository) List(ctx context.Context, serverID uuid.UUID, status *report.Status, limit, offset int) ([]report.Report, int, error) {
	var filter *string
	if status != nil {
		s := string(*status)
		filter = &s
	}

	var total int
	if err := r.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM reports WHERE server_id = $1 AND ($2::text IS NULL OR status = $2)`,
		serverID, filter,
	).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count reports: %w", err)
	}

	rows, err := r.pool.Query(ctx, `
		SELECT r.id, r.server_id, r.kind, r.reporter_id, r.channel_id, r.message_id, r.reported_user_id,
		       r.reason, r.status, r.created_at, r.resolved_at, r.resolved_by, r.resolution_action,
		       rp.user_id, rp.name, rp.avatar_url, rp.description, rp.status_text, rp.status_emoji, rp.version,
		       tp.user_id, tp.name, tp.avatar_url, tp.description, tp.status_text, tp.status_emoji, tp.version
		  FROM reports r
		  LEFT JOIN user_profiles rp ON rp.user_id = r.reporter_id
		  LEFT JOIN user_profiles tp ON tp.user_id = r.reported_user_id
		 WHERE r.server_id = $1 AND ($2::text IS NULL OR r.status = $2)
		 ORDER BY r.created_at DESC, r.id
		 LIMIT $3 OFFSET $4`,
		serverID, filter, limit, offset,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("list reports: %w", err)
	}
	defer rows.Close()

	reports := make([]report.Report, 0)
	for rows.Next() {
		var (
			rep      report.Report
			kind     string
			status   string
			action   *string
			reporter profileColumns
			reported profileColumns
		)
		if err := rows.Scan(
			&rep.ID, &rep.ServerID, &kind, &rep.ReporterID, &rep.ChannelID, &rep.MessageID, &rep.ReportedUserID,
			&rep.Reason, &status, &rep.CreatedAt, &rep.ResolvedAt, &rep.ResolvedBy, &action,
			&reporter.userID, &reporter.name, &reporter.avatarURL, &reporter.description, &reporter.statusText, &reporter.statusEmoji, &reporter.version,
			&reported.userID, &reported.name, &reported.avatarURL, &reported.description, &reported.statusText, &reported.statusEmoji, &reported.version,
		); err != nil {
			return nil, 0, fmt.Errorf("scan report: %w", err)
		}
		rep.Kind = report.Kind(kind)
		rep.Status = report.Status(status)
		rep.ResolutionAction = resolutionAction(action)
		rep.ReporterProfile = reporter.profile()
		rep.ReportedProfile = reported.profile()
		reports = append(reports, rep)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("list reports: %w", err)
	}
	return reports, total, nil
}

// Close moves a pending report to resolved or dismissed, with the optional
// action the moderator took, and writes servers.report_updated with the new
// snapshot in the same transaction. ErrReportNotFound / ErrReportNotPending /
// ErrReportActionKind publish nothing.
func (r *Repository) Close(ctx context.Context, serverID, reportID uuid.UUID, status report.Status, action *report.ResolutionAction, by string, at time.Time) (report.Report, error) {
	var rep report.Report
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		var (
			kind, st string
			stored   *string
		)
		if err := tx.QueryRow(ctx, `
			UPDATE reports
			   SET status = $3, resolved_by = $4, resolved_at = $5, resolution_action = $6
			 WHERE id = $1 AND server_id = $2 AND status = 'pending'
			RETURNING id, server_id, kind, reporter_id, channel_id, message_id, reported_user_id,
			          reason, status, created_at, resolved_at, resolved_by, resolution_action`,
			reportID, serverID, string(status), by, at, actionText(action),
		).Scan(
			&rep.ID, &rep.ServerID, &kind, &rep.ReporterID, &rep.ChannelID, &rep.MessageID, &rep.ReportedUserID,
			&rep.Reason, &st, &rep.CreatedAt, &rep.ResolvedAt, &rep.ResolvedBy, &stored,
		); err != nil {
			return err
		}
		rep.Kind = report.Kind(kind)
		rep.Status = report.Status(st)
		rep.ResolutionAction = resolutionAction(stored)

		event, err := UpdatedEvent(rep)
		if err != nil {
			return err
		}
		return outbox.Insert(ctx, tx, event)
	})
	if err == nil {
		return rep, nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == pgCheckViolation && pgErr.ConstraintName == actionKindConstraint {
		return report.Report{}, model.ErrReportActionKind
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return report.Report{}, fmt.Errorf("close report: %w", err)
	}

	var exists bool
	if err := r.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM reports WHERE id = $1 AND server_id = $2)`, reportID, serverID,
	).Scan(&exists); err != nil {
		return report.Report{}, fmt.Errorf("check report: %w", err)
	}
	if !exists {
		return report.Report{}, model.ErrReportNotFound
	}
	return report.Report{}, model.ErrReportNotPending
}

func actionText(a *report.ResolutionAction) *string {
	if a == nil {
		return nil
	}
	s := string(*a)
	return &s
}

func resolutionAction(raw *string) *report.ResolutionAction {
	if raw == nil {
		return nil
	}
	a := report.ResolutionAction(*raw)
	return &a
}

type profileColumns struct {
	userID, name, avatarURL, description, statusText, statusEmoji *string
	version                                                       *int64
}

func (p profileColumns) profile() *userprofile.Profile {
	if p.userID == nil {
		return nil
	}
	return &userprofile.Profile{
		UserID: *p.userID, Name: *p.name, AvatarURL: *p.avatarURL, Description: *p.description,
		StatusText: *p.statusText, StatusEmoji: *p.statusEmoji, Version: *p.version,
	}
}
