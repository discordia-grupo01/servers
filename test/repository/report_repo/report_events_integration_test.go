package report_repo_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/discordia-grupo01/events-contract/reports"
	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/model/report"
)

type outboxRow struct {
	routingKey string
	payload    []byte
}

func outboxRows(ctx context.Context, t *testing.T, pool *pgxpool.Pool) []outboxRow {
	t.Helper()
	rows, err := pool.Query(ctx, `SELECT routing_key, payload FROM outbox_events ORDER BY created_at, id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []outboxRow
	for rows.Next() {
		var r outboxRow
		if err := rows.Scan(&r.routingKey, &r.payload); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func updates(ctx context.Context, t *testing.T, pool *pgxpool.Pool) []reports.ReportUpdated {
	t.Helper()
	var out []reports.ReportUpdated
	for _, r := range outboxRows(ctx, t, pool) {
		if r.routingKey != reports.RoutingKeyReportUpdated {
			t.Fatalf("routing key = %q, want %q", r.routingKey, reports.RoutingKeyReportUpdated)
		}
		var e reports.ReportUpdated
		if err := json.Unmarshal(r.payload, &e); err != nil {
			t.Fatal(err)
		}
		out = append(out, e)
	}
	return out
}

func TestCreate_PublishesAPendingSnapshotInTheSameTransaction(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	serverID := seedServer(ctx, t, pool)
	channelID := seedChannel(ctx, t, pool, serverID)
	now := time.Now().UTC().Truncate(time.Microsecond)

	msg := messageReport(serverID, channelID, "reporter-1", now)
	usr := userReport(serverID, "reporter-1", "target-1", now.Add(time.Second))
	for _, rep := range []report.Report{msg, usr} {
		if err := repo.Create(ctx, rep); err != nil {
			t.Fatal(err)
		}
	}

	got := updates(ctx, t, pool)
	if len(got) != 2 {
		t.Fatalf("events = %d, want 2", len(got))
	}
	m, u := got[0], got[1]
	if m.ReportID != msg.ID.String() || m.ServerID != serverID.String() || m.Kind != reports.KindMessage ||
		m.Status != reports.StatusPending || m.Reason != "spam" || m.ReportedUserID != nil || m.ResolvedAt != nil ||
		m.ChannelID == nil || *m.ChannelID != channelID.String() || m.MessageID == nil || *m.MessageID != msg.MessageID.String() ||
		!m.CreatedAt.Equal(now) || !m.OccurredAt.Equal(now) || m.EventID == "" {
		t.Errorf("message event = %+v", m)
	}
	if u.ReportID != usr.ID.String() || u.Kind != reports.KindUser || u.ReportedUserID == nil || *u.ReportedUserID != "target-1" ||
		u.ChannelID != nil || u.MessageID != nil || u.Status != reports.StatusPending || !u.OccurredAt.Equal(now.Add(time.Second)) {
		t.Errorf("user event = %+v", u)
	}
}

func TestCreate_APendingDuplicatePublishesNothing(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	serverID := seedServer(ctx, t, pool)
	rep := userReport(serverID, "reporter-1", "target-1", time.Now().UTC())
	if err := repo.Create(ctx, rep); err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(ctx, userReport(serverID, "reporter-1", "target-1", time.Now().UTC())); !errors.Is(err, model.ErrReportAlreadyPending) {
		t.Fatalf("err = %v, want ErrReportAlreadyPending", err)
	}
	if got := updates(ctx, t, pool); len(got) != 1 {
		t.Errorf("events = %d, want only the first report's", len(got))
	}
}

func TestClose_PublishesTheClosedSnapshot(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	serverID := seedServer(ctx, t, pool)
	now := time.Now().UTC().Truncate(time.Microsecond)
	rep := userReport(serverID, "reporter-1", "target-1", now)
	if err := repo.Create(ctx, rep); err != nil {
		t.Fatal(err)
	}

	closedAt := now.Add(time.Hour)
	if _, err := repo.Close(ctx, serverID, rep.ID, report.StatusResolved, "moderator-1", closedAt); err != nil {
		t.Fatal(err)
	}

	got := updates(ctx, t, pool)
	if len(got) != 2 {
		t.Fatalf("events = %d, want 2 (created + closed)", len(got))
	}
	e := got[1]
	if e.ReportID != rep.ID.String() || e.Status != reports.StatusResolved || e.ResolvedAt == nil || !e.ResolvedAt.Equal(closedAt) ||
		!e.OccurredAt.Equal(closedAt) || !e.CreatedAt.Equal(now) || e.Reason != "acoso" {
		t.Errorf("closed event = %+v", e)
	}
	if !got[0].OccurredAt.Before(e.OccurredAt) {
		t.Error("the closing snapshot must be newer than the creation one, or admin would ignore it")
	}
}

func TestClose_NotFoundAndNotPendingPublishNothingNew(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	serverID := seedServer(ctx, t, pool)
	now := time.Now().UTC()
	rep := userReport(serverID, "reporter-1", "target-1", now)
	if err := repo.Create(ctx, rep); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Close(ctx, serverID, rep.ID, report.StatusDismissed, "moderator-1", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	before := len(updates(ctx, t, pool))

	if _, err := repo.Close(ctx, serverID, rep.ID, report.StatusResolved, "moderator-2", now.Add(time.Hour)); !errors.Is(err, model.ErrReportNotPending) {
		t.Fatalf("closing twice: err = %v, want ErrReportNotPending", err)
	}
	if _, err := repo.Close(ctx, serverID, uuid.New(), report.StatusResolved, "moderator-1", now); !errors.Is(err, model.ErrReportNotFound) {
		t.Fatalf("unknown report: err = %v, want ErrReportNotFound", err)
	}
	if after := len(updates(ctx, t, pool)); after != before {
		t.Errorf("events = %d, want %d: a failed close must not publish", after, before)
	}
}

func TestClose_ConcurrentClosesPublishOnce(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	serverID := seedServer(ctx, t, pool)
	now := time.Now().UTC()
	rep := userReport(serverID, "reporter-1", "target-1", now)
	if err := repo.Create(ctx, rep); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	results := make([]error, 4)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, results[i] = repo.Close(ctx, serverID, rep.ID, report.StatusResolved, "moderator", now.Add(time.Minute))
		}()
	}
	wg.Wait()

	wins := 0
	for _, err := range results {
		if err == nil {
			wins++
		} else if !errors.Is(err, model.ErrReportNotPending) {
			t.Errorf("unexpected error: %v", err)
		}
	}
	if wins != 1 {
		t.Errorf("successful closes = %d, want 1", wins)
	}
	if got := updates(ctx, t, pool); len(got) != 2 {
		t.Errorf("events = %d, want 2 (created + one close)", len(got))
	}
}

func TestEvents_NeverCarryWhoReportedOrResolved(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	serverID := seedServer(ctx, t, pool)
	now := time.Now().UTC()
	rep := userReport(serverID, "secret-reporter", "target-1", now)
	if err := repo.Create(ctx, rep); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Close(ctx, serverID, rep.ID, report.StatusResolved, "secret-moderator", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	for _, r := range outboxRows(ctx, t, pool) {
		body := string(r.payload)
		for _, forbidden := range []string{"secret-reporter", "secret-moderator", "reporter_id", "resolved_by"} {
			if strings.Contains(body, forbidden) {
				t.Errorf("payload contains %q: %s", forbidden, body)
			}
		}
	}
}
