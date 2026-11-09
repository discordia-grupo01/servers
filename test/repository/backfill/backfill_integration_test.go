package backfill_test

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	moderationevents "github.com/discordia-grupo01/events-contract/moderation"
	"github.com/discordia-grupo01/events-contract/reports"
	"github.com/discordia-grupo01/servers/internal/repository"
)

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func newPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	host := os.Getenv("DATABASE_HOST")
	if host == "" {
		t.Skip("DATABASE_HOST not set: skipping integration test")
	}
	dsn := "host=" + host +
		" port=" + env("DATABASE_PORT", "5432") +
		" dbname=" + env("DATABASE_NAME", "discordia_servicio") +
		" user=" + env("DATABASE_USER", "discordia") +
		" password=" + env("DATABASE_PASSWORD", "discordia") +
		" sslmode=disable"

	ctx := context.Background()
	pool, err := repository.NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	t.Cleanup(pool.Close)
	drop := func() {
		_, _ = pool.Exec(ctx, `DROP TABLE IF EXISTS moderation_audit_entries, reports, outbox_events, user_profiles, server_members, channels, servers CASCADE`)
	}
	t.Cleanup(drop)
	drop()
	apply(ctx, t, pool,
		"0001_init", "0011_outbox_events", "0033_outbox_traceparent", "0017_user_profiles", "0029_reports", "0032_moderation_audit")
	return pool
}

func apply(ctx context.Context, t *testing.T, pool *pgxpool.Pool, names ...string) {
	t.Helper()
	for _, name := range names {
		sql, err := os.ReadFile("../../../migrations/" + name + ".up.sql")
		if err != nil {
			t.Fatalf("read migration %s: %v", name, err)
		}
		if _, err := pool.Exec(ctx, string(sql)); err != nil {
			t.Fatalf("apply migration %s: %v", name, err)
		}
	}
}

func exec(ctx context.Context, t *testing.T, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(ctx, sql, args...); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
}

func payloads(ctx context.Context, t *testing.T, pool *pgxpool.Pool, routingKey string) [][]byte {
	t.Helper()
	rows, err := pool.Query(ctx, `SELECT payload FROM outbox_events WHERE routing_key = $1 ORDER BY id`, routingKey)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out [][]byte
	for rows.Next() {
		var p []byte
		if err := rows.Scan(&p); err != nil {
			t.Fatal(err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestBackfillReportsPublishesTheCurrentSnapshotOfEveryReport(t *testing.T) {
	pool := newPool(t)
	ctx := context.Background()
	serverID, channelID, messageID := uuid.New(), uuid.New(), uuid.New()
	pendingID, resolvedID := uuid.New(), uuid.New()
	created := time.Date(2026, 10, 1, 10, 0, 0, 123456000, time.UTC)
	resolved := created.Add(time.Hour)

	exec(ctx, t, pool, `INSERT INTO servers (id, name, name_key, owner_id) VALUES ($1, 'Sala', 'sala', 'owner-1')`, serverID)
	exec(ctx, t, pool, `INSERT INTO channels (id, server_id, name, kind) VALUES ($1, $2, 'general', 'text')`, channelID, serverID)
	exec(ctx, t, pool, `
		INSERT INTO reports (id, server_id, kind, reporter_id, channel_id, message_id, reason, status, created_at)
		VALUES ($1, $2, 'message', 'secret-reporter', $3, $4, 'spam', 'pending', $5)`, pendingID, serverID, channelID, messageID, created)
	exec(ctx, t, pool, `
		INSERT INTO reports (id, server_id, kind, reporter_id, reported_user_id, reason, status, created_at, resolved_at, resolved_by)
		VALUES ($1, $2, 'user', 'secret-reporter', 'target-1', 'acoso', 'resolved', $3, $4, 'secret-moderator')`, resolvedID, serverID, created, resolved)

	sql, err := os.ReadFile("../../../migrations/0034_backfill_report_events.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	before := time.Now().UTC().Add(-time.Second)
	if _, err := pool.Exec(ctx, string(sql)); err != nil {
		t.Fatalf("0034: %v", err)
	}

	raw := payloads(ctx, t, pool, reports.RoutingKeyReportUpdated)
	if len(raw) != 2 {
		t.Fatalf("events = %d, want one per report", len(raw))
	}
	byID := map[string]reports.ReportUpdated{}
	for _, p := range raw {
		body := string(p)
		for _, forbidden := range []string{"secret-reporter", "secret-moderator", "reporter_id", "resolved_by"} {
			if strings.Contains(body, forbidden) {
				t.Errorf("payload contains %q: %s", forbidden, body)
			}
		}
		var e reports.ReportUpdated
		if err := json.Unmarshal(p, &e); err != nil {
			t.Fatalf("payload doesn't decode with the contract type: %v: %s", err, body)
		}
		// Missing values are an explicit null, not an absent key.
		var keys map[string]json.RawMessage
		if err := json.Unmarshal(p, &keys); err != nil {
			t.Fatal(err)
		}
		for _, k := range []string{"reported_user_id", "channel_id", "message_id", "resolved_at"} {
			if _, ok := keys[k]; !ok {
				t.Errorf("key %q missing from %s", k, body)
			}
		}
		if e.EventID == "" || e.OccurredAt.Before(before) {
			t.Errorf("occurred_at = %v: the backfill must be newer than any earlier live event", e.OccurredAt)
		}
		byID[e.ReportID] = e
	}

	p := byID[pendingID.String()]
	if p.Kind != "message" || p.Status != "pending" || p.ServerID != serverID.String() || p.Reason != "spam" ||
		p.ChannelID == nil || *p.ChannelID != channelID.String() || p.MessageID == nil || *p.MessageID != messageID.String() ||
		p.ReportedUserID != nil || p.ResolvedAt != nil || !p.CreatedAt.Equal(created) {
		t.Errorf("pending message report = %+v", p)
	}
	r := byID[resolvedID.String()]
	if r.Kind != "user" || r.Status != "resolved" || r.ReportedUserID == nil || *r.ReportedUserID != "target-1" ||
		r.ChannelID != nil || r.MessageID != nil || r.ResolvedAt == nil || !r.ResolvedAt.Equal(resolved) || !r.CreatedAt.Equal(created) {
		t.Errorf("resolved user report = %+v", r)
	}
}

func TestBackfillModerationActionsKeepsTheOriginalIdAndTime(t *testing.T) {
	pool := newPool(t)
	ctx := context.Background()
	serverID := uuid.New()
	banID, unmuteID := uuid.New(), uuid.New()
	banAt := time.Date(2026, 9, 5, 14, 30, 0, 654321000, time.UTC)
	unmuteAt := banAt.Add(24 * time.Hour)

	exec(ctx, t, pool, `INSERT INTO servers (id, name, name_key, owner_id) VALUES ($1, 'Sala', 'sala', 'owner-1')`, serverID)
	exec(ctx, t, pool, `
		INSERT INTO moderation_audit_entries (id, server_id, action, actor_user_id, target_user_id, occurred_at)
		VALUES ($1, $2, 'ban', 'secret-moderator', 'target-1', $3), ($4, $2, 'unmute', 'system', 'target-2', $5)`,
		banID, serverID, banAt, unmuteID, unmuteAt)

	sql, err := os.ReadFile("../../../migrations/0035_backfill_moderation_actions.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	// Running it twice repeats the same facts (same action_id); admin deduplicates.
	for i := 0; i < 2; i++ {
		if _, err := pool.Exec(ctx, string(sql)); err != nil {
			t.Fatalf("0035: %v", err)
		}
	}

	raw := payloads(ctx, t, pool, moderationevents.RoutingKeyModerationActionRecorded)
	if len(raw) != 4 {
		t.Fatalf("events = %d, want 2 per run", len(raw))
	}
	seen := map[string]moderationevents.ModerationActionRecorded{}
	for _, p := range raw {
		body := string(p)
		for _, forbidden := range []string{"secret-moderator", "system", "actor"} {
			if strings.Contains(body, forbidden) {
				t.Errorf("payload contains %q: %s", forbidden, body)
			}
		}
		var e moderationevents.ModerationActionRecorded
		if err := json.Unmarshal(p, &e); err != nil {
			t.Fatalf("payload doesn't decode with the contract type: %v: %s", err, body)
		}
		seen[e.ActionID] = e
	}
	if len(seen) != 2 {
		t.Fatalf("distinct action_ids = %d, want 2", len(seen))
	}
	if e := seen[banID.String()]; e.Action != "ban" || e.TargetUserID != "target-1" || e.ServerID != serverID.String() || !e.OccurredAt.Equal(banAt) {
		t.Errorf("ban = %+v", e)
	}
	if e := seen[unmuteID.String()]; e.Action != "unmute" || e.TargetUserID != "target-2" || !e.OccurredAt.Equal(unmuteAt) {
		t.Errorf("unmute = %+v", e)
	}
}

func TestBackfillsOnAnEmptyDatabasePublishNothing(t *testing.T) {
	pool := newPool(t)
	ctx := context.Background()
	apply(ctx, t, pool, "0034_backfill_report_events", "0035_backfill_moderation_actions")
	var n int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM outbox_events`).Scan(&n); err != nil || n != 0 {
		t.Errorf("events = %d, err = %v, want 0", n, err)
	}
}
