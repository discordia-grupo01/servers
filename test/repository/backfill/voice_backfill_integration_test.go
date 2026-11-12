package backfill_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/discordia-grupo01/events-contract/channels"
	"github.com/discordia-grupo01/events-contract/moderation"
	"github.com/discordia-grupo01/events-contract/permissions"
	"github.com/discordia-grupo01/servers/internal/model/role"
	"github.com/discordia-grupo01/servers/internal/repository"
	"github.com/discordia-grupo01/servers/internal/voicebackfill"
)

type capturedPublisher struct {
	events []any
}

func (p *capturedPublisher) Publish(_ context.Context, payload any) error {
	p.events = append(p.events, payload)
	return nil
}

func TestRunPublishesCompleteVoiceAuthorizationSnapshots(t *testing.T) {
	pool := newVoiceBackfillPool(t)
	ctx := context.Background()
	serverID := uuid.New()
	voiceID, textID := uuid.New(), uuid.New()
	everyoneID, roleID := uuid.New(), uuid.New()
	userID := "member-1"
	base := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	roleChanged := base.Add(time.Hour)
	memberChanged := roleChanged.Add(time.Hour)
	overrideChanged := memberChanged.Add(time.Hour)
	muteChanged := overrideChanged.Add(time.Hour)
	muteUntil := muteChanged.Add(time.Hour)

	execVoiceBackfill(t, pool,
		`INSERT INTO servers (id, name, name_key, owner_id, created_at) VALUES ($1, 'Sala', 'sala', 'owner-1', $2)`,
		serverID, base)
	execVoiceBackfill(t, pool,
		`INSERT INTO channels (id, server_id, name, kind, created_at) VALUES ($1, $2, 'voz', 'voice', $3), ($4, $2, 'texto', 'text', $3)`,
		voiceID, serverID, base, textID)
	execVoiceBackfill(t, pool,
		`INSERT INTO roles (id, server_id, name, color, permissions, position, is_everyone, created_at, updated_at)
		 VALUES ($1, $3, '@everyone', '#000000', $4, 0, true, $5, $5),
		        ($2, $3, 'Voice', '#000000', $6, 1, false, $5, $7)`,
		everyoneID, roleID, serverID, role.PermViewChannels, base,
		role.PermConnect, roleChanged)
	execVoiceBackfill(t, pool,
		`INSERT INTO server_members (server_id, user_id, joined_at) VALUES ($1, $2, $3)`,
		serverID, userID, base)
	execVoiceBackfill(t, pool,
		`INSERT INTO member_roles (server_id, user_id, role_id, assigned_at) VALUES ($1, $2, $3, $4), ($1, $2, $5, $6)`,
		serverID, userID, everyoneID, base, roleID, memberChanged)
	execVoiceBackfill(t, pool,
		`INSERT INTO channel_permission_overrides (channel_id, role_id, allow, deny, updated_at)
		 VALUES ($1, $2, $3, 0, $4)`,
		voiceID, roleID, role.PermConnect, overrideChanged)
	execVoiceBackfill(t, pool,
		`INSERT INTO server_mutes (server_id, user_id, muted_by, muted_at, until_at, reason)
		 VALUES ($1, $2, 'moderator-1', $3, $4, 'test')`,
		serverID, userID, muteChanged, muteUntil)
	execVoiceBackfill(t, pool,
		`INSERT INTO ownership_transfers (id, server_id, from_user_id, to_user_id, status, resolved_at)
		 VALUES ($1, $2, 'owner-1', 'owner-2', 'accepted', $3)`,
		uuid.New(), serverID, roleChanged)

	publisher := &capturedPublisher{}
	counts, err := voicebackfill.Run(ctx, pool, publisher)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if counts.Channels != 1 || counts.Roles != 2 || counts.Owners != 1 ||
		counts.Members != 1 || counts.Overrides != 1 || counts.Mutes != 1 {
		t.Fatalf("counts = %+v, want one voice snapshot per category", counts)
	}
	if len(publisher.events) != 7 {
		t.Fatalf("published events = %d, want 7", len(publisher.events))
	}

	var channel channels.ChannelCreated
	var member permissions.MemberRolesUpdated
	var overrides permissions.ChannelOverridesUpdated
	var mute moderation.MemberMuted
	for _, event := range publisher.events {
		switch value := event.(type) {
		case channels.ChannelCreated:
			channel = value
		case permissions.MemberRolesUpdated:
			member = value
		case permissions.ChannelOverridesUpdated:
			overrides = value
		case moderation.MemberMuted:
			mute = value
		}
	}
	if channel.ChannelID != voiceID.String() || channel.Kind != "voice" || !channel.Meta.OccurredAt.Equal(base) {
		t.Errorf("channel snapshot = %+v", channel)
	}
	if member.UserID != userID || len(member.RoleIDs) != 2 || !member.Meta.OccurredAt.Equal(memberChanged) {
		t.Errorf("member snapshot = %+v", member)
	}
	if overrides.ChannelID != voiceID.String() || len(overrides.Overrides) != 1 ||
		!overrides.Meta.OccurredAt.Equal(overrideChanged) {
		t.Errorf("override snapshot = %+v", overrides)
	}
	if mute.UserID != userID || !mute.UntilAt.Equal(muteUntil) || !mute.Meta.OccurredAt.Equal(muteChanged) {
		t.Errorf("mute snapshot = %+v", mute)
	}
}

func newVoiceBackfillPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	host := os.Getenv("DATABASE_HOST")
	if host == "" {
		t.Skip("DATABASE_HOST not set: skipping integration test")
	}
	dsn := "host=" + host +
		" port=" + envVoiceBackfill("DATABASE_PORT", "5432") +
		" dbname=" + envVoiceBackfill("DATABASE_NAME", "discordia_servicio") +
		" user=" + envVoiceBackfill("DATABASE_USER", "discordia") +
		" password=" + envVoiceBackfill("DATABASE_PASSWORD", "discordia") +
		" sslmode=" + envVoiceBackfill("DATABASE_SSLMODE", "disable")
	pool, err := repository.NewPool(context.Background(), dsn)
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	t.Cleanup(pool.Close)
	_, _ = pool.Exec(context.Background(), `
			DROP TABLE IF EXISTS outbox_events, server_mutes, channel_permission_overrides, member_roles,
			ownership_transfers, roles, server_members, channels, servers CASCADE`)
	for _, migration := range []string{
		"0001_init", "0003_roles", "0004_member_roles", "0006_ownership_transfers",
		/* agregar aquí la migración que crea outbox_events, ej. "00XX_outbox_events", */
		"0015_role_positions", "0019_everyone_role", "0020_channel_permission_overrides",
		"0021_server_mutes", "0025_connect_permission",
	} {
		sql, err := os.ReadFile("../../../migrations/" + migration + ".up.sql")
		if err != nil {
			t.Fatalf("read migration %s: %v", migration, err)
		}
		if _, err := pool.Exec(context.Background(), string(sql)); err != nil {
			t.Fatalf("apply migration %s: %v", migration, err)
		}
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `
				DROP TABLE IF EXISTS outbox_events, server_mutes, channel_permission_overrides, member_roles,
				ownership_transfers, roles, server_members, channels, servers CASCADE`)
	})
	return pool
}

func execVoiceBackfill(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("exec %s: %v", sql, err)
	}
}

func envVoiceBackfill(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
