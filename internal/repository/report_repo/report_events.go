package report_repo

import (
	"time"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/events-contract/envelope"
	reportevents "github.com/discordia-grupo01/events-contract/reports"
	"github.com/discordia-grupo01/servers/internal/model/report"
	"github.com/discordia-grupo01/servers/internal/outbox"
)

// UpdatedEvent builds the servers.report_updated event: the full snapshot of the
// report as it is now. Admin keeps the snapshot with the newest occurred_at,
// which is when the report was created or, once closed, when it was resolved.
//
// The event never carries reporter_id or resolved_by: admin doesn't need to
// know who reported or who resolved, and leaving them out keeps that data in
// this service (data minimisation).
func UpdatedEvent(rep report.Report) (outbox.Event, error) {
	occurredAt := rep.CreatedAt
	if rep.ResolvedAt != nil {
		occurredAt = *rep.ResolvedAt
	}
	return outbox.New(reportevents.RoutingKeyReportUpdated, reportevents.ReportUpdated{
		Meta:           envelope.Meta{EventID: uuid.NewString(), OccurredAt: occurredAt.UTC()},
		ReportID:       rep.ID.String(),
		ServerID:       rep.ServerID.String(),
		Kind:           string(rep.Kind),
		ReportedUserID: rep.ReportedUserID,
		ChannelID:      uuidPtr(rep.ChannelID),
		MessageID:      uuidPtr(rep.MessageID),
		Reason:         rep.Reason,
		Status:         string(rep.Status),
		CreatedAt:      rep.CreatedAt.UTC(),
		ResolvedAt:     utcPtr(rep),
	})
}

func uuidPtr(id *uuid.UUID) *string {
	if id == nil {
		return nil
	}
	s := id.String()
	return &s
}

func utcPtr(rep report.Report) *time.Time {
	if rep.ResolvedAt == nil {
		return nil
	}
	t := rep.ResolvedAt.UTC()
	return &t
}
