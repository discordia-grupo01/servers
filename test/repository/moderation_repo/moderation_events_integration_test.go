package moderation_repo_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	moderationevents "github.com/discordia-grupo01/events-contract/moderation"
	"github.com/discordia-grupo01/servers/internal/model/moderation"
	"github.com/discordia-grupo01/servers/internal/model/role"
)

func recordedActions(ctx context.Context, t *testing.T, pool *pgxpool.Pool) []moderationevents.ModerationActionRecorded {
	t.Helper()
	rows, err := pool.Query(ctx,
		`SELECT payload FROM outbox_events WHERE routing_key = $1 ORDER BY created_at, id`,
		moderationevents.RoutingKeyModerationActionRecorded)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []moderationevents.ModerationActionRecorded
	for rows.Next() {
		var payload []byte
		if err := rows.Scan(&payload); err != nil {
			t.Fatal(err)
		}
		var e moderationevents.ModerationActionRecorded
		if err := json.Unmarshal(payload, &e); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(payload), "mod-1") || strings.Contains(string(payload), "actor") {
			t.Errorf("the event must not carry the actor: %s", payload)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func auditEntry(action, target string, at time.Time) moderation.AuditEntry {
	return moderation.AuditEntry{ID: uuid.New(), Action: action, ActorUserID: "mod-1", TargetUserID: target, OccurredAt: at}
}

func assertRecorded(t *testing.T, got []moderationevents.ModerationActionRecorded, entry moderation.AuditEntry, serverID uuid.UUID) {
	t.Helper()
	if len(got) != 1 {
		t.Fatalf("moderation_action_recorded events = %d, want 1", len(got))
	}
	e := got[0]
	if e.ActionID != entry.ID.String() || e.ServerID != serverID.String() || e.Action != entry.Action ||
		e.TargetUserID != entry.TargetUserID || !e.OccurredAt.Equal(entry.OccurredAt) || e.EventID == "" {
		t.Errorf("event = %+v, want action_id=%s server=%s action=%s target=%s at=%s",
			e, entry.ID, serverID, entry.Action, entry.TargetUserID, entry.OccurredAt)
	}
}

// Every audited action publishes exactly one event, with the audit row's id as
// action_id so the consumer can deduplicate.
func TestAuditedActionsPublishModerationActionRecorded(t *testing.T) {
	at := time.Date(2026, 11, 2, 12, 0, 0, 123000000, time.UTC)

	t.Run("ban", func(t *testing.T) {
		repo, pool := newRepo(t)
		ctx := context.Background()
		serverID := seedServer(ctx, t, pool, "target")
		entry := auditEntry(moderation.AuditActionBan, "target", at)
		if err := repo.BanMember(moderation.WithAudit(ctx, entry), newBan(serverID, "target"), allowAll); err != nil {
			t.Fatal(err)
		}
		assertRecorded(t, recordedActions(ctx, t, pool), entry, serverID)
	})

	t.Run("kick", func(t *testing.T) {
		repo, pool := newRepo(t)
		ctx := context.Background()
		serverID := seedServer(ctx, t, pool, "target")
		entry := auditEntry(moderation.AuditActionKick, "target", at)
		if err := repo.KickMember(moderation.WithAudit(ctx, entry), newKick(serverID, "target"), allowAll); err != nil {
			t.Fatal(err)
		}
		assertRecorded(t, recordedActions(ctx, t, pool), entry, serverID)
	})

	t.Run("mute", func(t *testing.T) {
		repo, pool := newRepo(t)
		ctx := context.Background()
		serverID := seedServer(ctx, t, pool, "target")
		entry := auditEntry(moderation.AuditActionMute, "target", at)
		if err := repo.MuteMember(moderation.WithAudit(ctx, entry), newMute(serverID, "target", at, at.Add(time.Hour)), allowAll); err != nil {
			t.Fatal(err)
		}
		assertRecorded(t, recordedActions(ctx, t, pool), entry, serverID)
	})

	t.Run("unban", func(t *testing.T) {
		repo, pool := newRepo(t)
		ctx := context.Background()
		serverID := seedServer(ctx, t, pool, "target")
		if err := repo.BanMember(ctx, newBan(serverID, "target"), allowAll); err != nil { // without audit: nothing published
			t.Fatal(err)
		}
		entry := auditEntry(moderation.AuditActionUnban, "target", at)
		if err := repo.UnbanMember(moderation.WithAudit(ctx, entry), serverID, "target"); err != nil {
			t.Fatal(err)
		}
		assertRecorded(t, recordedActions(ctx, t, pool), entry, serverID)
	})

	t.Run("unmute", func(t *testing.T) {
		repo, pool := newRepo(t)
		ctx := context.Background()
		serverID := seedServer(ctx, t, pool, "target")
		if err := repo.MuteMember(ctx, newMute(serverID, "target", at, at.Add(time.Hour)), allowAll); err != nil {
			t.Fatal(err)
		}
		entry := auditEntry(moderation.AuditActionUnmute, "target", at)
		if err := repo.UnmuteMember(moderation.WithAudit(ctx, entry), serverID, "target"); err != nil {
			t.Fatal(err)
		}
		assertRecorded(t, recordedActions(ctx, t, pool), entry, serverID)
	})
}

// The automatic unmute when a mute expires goes through insertAudit too, so it
// is published like any other action.
func TestExpiredMuteAlsoPublishesModerationActionRecorded(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	serverID := seedServer(ctx, t, pool, "expired")
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	exec(ctx, t, pool, `
		INSERT INTO server_mutes (server_id, user_id, muted_by, muted_at, until_at, reason)
		VALUES ($1, 'expired', 'mod-1', $2, $3, 'spam')`, serverID, now.Add(-time.Hour), now.Add(-time.Minute))

	if _, err := repo.ExpireMutes(ctx, now); err != nil {
		t.Fatal(err)
	}

	got := recordedActions(ctx, t, pool)
	if len(got) != 1 {
		t.Fatalf("events = %d, want 1", len(got))
	}
	var auditID uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT id FROM moderation_audit_entries WHERE server_id = $1`, serverID).Scan(&auditID); err != nil {
		t.Fatal(err)
	}
	if got[0].ActionID != auditID.String() || got[0].Action != moderation.AuditActionUnmute ||
		got[0].TargetUserID != "expired" || !got[0].OccurredAt.Equal(now) {
		t.Errorf("event = %+v, want unmute of 'expired' with action_id %s", got[0], auditID)
	}
}

// A rejected or repeated action leaves neither an audit row nor an event.
func TestRejectedActionsPublishNothing(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	serverID := seedServer(ctx, t, pool, "target")
	at := time.Now().UTC()

	entry := auditEntry(moderation.AuditActionBan, "target", at)
	rejectAll := func(_, _ role.Standing) error { return errors.New("rejected") }
	if err := repo.BanMember(moderation.WithAudit(ctx, entry), newBan(serverID, "target"), rejectAll); err == nil {
		t.Fatal("the check should have rejected the ban")
	}
	// Unbanning someone who isn't banned.
	if err := repo.UnbanMember(moderation.WithAudit(ctx, auditEntry(moderation.AuditActionUnban, "target", at)), serverID, "target"); err == nil {
		t.Fatal("unbanning without a ban should fail")
	}
	if got := recordedActions(ctx, t, pool); len(got) != 0 {
		t.Errorf("events = %+v, want none", got)
	}
	if n := count(ctx, t, pool, `SELECT COUNT(*) FROM moderation_audit_entries`); n != 0 {
		t.Errorf("audit rows = %d, want 0", n)
	}
}

// An action the contract doesn't know can't be audited: the whole transaction
// rolls back, so there is never an audit row admin wouldn't hear about.
func TestUnknownAuditActionRollsBack(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	serverID := seedServer(ctx, t, pool, "target")
	entry := auditEntry("banish", "target", time.Now().UTC())

	if err := repo.BanMember(moderation.WithAudit(ctx, entry), newBan(serverID, "target"), allowAll); err == nil {
		t.Fatal("an action without an event should fail")
	}
	if n := count(ctx, t, pool, `SELECT COUNT(*) FROM server_bans`); n != 0 {
		t.Errorf("bans = %d: the ban must roll back", n)
	}
	if n := count(ctx, t, pool, `SELECT COUNT(*) FROM server_members WHERE server_id = $1 AND user_id = 'target'`, serverID); n != 1 {
		t.Error("the member must still be there")
	}
	if n := count(ctx, t, pool, `SELECT COUNT(*) FROM moderation_audit_entries`); n != 0 {
		t.Errorf("audit rows = %d, want 0", n)
	}
}

// The contract and this service must spell the five actions the same way.
func TestAuditActionsMatchTheContract(t *testing.T) {
	pairs := map[string]string{
		moderation.AuditActionBan:    moderationevents.ActionBan,
		moderation.AuditActionUnban:  moderationevents.ActionUnban,
		moderation.AuditActionKick:   moderationevents.ActionKick,
		moderation.AuditActionMute:   moderationevents.ActionMute,
		moderation.AuditActionUnmute: moderationevents.ActionUnmute,
	}
	for ours, theirs := range pairs {
		if ours != theirs {
			t.Errorf("audit action %q != contract action %q", ours, theirs)
		}
	}
}
