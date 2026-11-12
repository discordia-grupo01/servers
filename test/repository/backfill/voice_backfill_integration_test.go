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
	voiceID, emptyVoiceID, textID := uuid.New(), uuid.New(), uuid.New()
	everyoneID, roleID := uuid.New(), uuid.New()
	userID := "member-1"
	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	roleChanged := base.Add(time.Hour)
	memberChanged := roleChanged.Add(time.Hour)
	overrideChanged := memberChanged.Add(time.Hour)
	muteChanged := overrideChanged.Add(time.Hour)
	muteUntil := muteChanged.Add(time.Hour)

	execVoiceBackfill(t, pool,
		`INSERT INTO servers (id, name, name_key, owner_id, created_at) VALUES ($1, 'Sala', 'sala', 'owner-1', $2)`,
		serverID, base)
	execVoiceBackfill(t, pool,
		`INSERT INTO channels (id, server_id, name, kind, created_at)
		 VALUES ($1, $2, 'voz', 'voice', $3), ($4, $2, 'voz-vacia', 'voice', $3), ($5, $2, 'texto', 'text', $3)`,
		voiceID, serverID, base, emptyVoiceID, textID)
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
	if counts.Channels != 2 || counts.Roles != 2 || counts.Owners != 1 ||
		counts.Members != 1 || counts.Overrides != 2 || counts.Mutes != 1 {
		t.Fatalf("counts = %+v, want one voice snapshot per category", counts)
	}
	if len(publisher.events) != 8 {
		t.Fatalf("published events = %d, want 8", len(publisher.events))
	}

	channelsByID := make(map[string]channels.ChannelCreated)
	var member permissions.MemberRolesUpdated
	overridesByChannelID := make(map[string]permissions.ChannelOverridesUpdated)
	var mute moderation.MemberMuted
	for _, event := range publisher.events {
		switch value := event.(type) {
		case channels.ChannelCreated:
			channelsByID[value.ChannelID] = value
		case permissions.MemberRolesUpdated:
			member = value
		case permissions.ChannelOverridesUpdated:
			overridesByChannelID[value.ChannelID] = value
		case moderation.MemberMuted:
			mute = value
		}
	}
	channel := channelsByID[voiceID.String()]
	if channel.ChannelID != voiceID.String() {
		t.Errorf("channel id = %s, want %s", channel.ChannelID, voiceID)
	}
	if channel.Kind != "voice" {
		t.Errorf("channel kind = %s, want voice", channel.Kind)
	}
	if !channel.Meta.OccurredAt.Equal(base) {
		t.Errorf("channel occurred_at = %s (%d), want %s (%d)",
			channel.Meta.OccurredAt, channel.Meta.OccurredAt.UnixNano(), base, base.UnixNano())
	}
	if member.UserID != userID {
		t.Errorf("member user id = %s, want %s", member.UserID, userID)
	}
	if len(member.RoleIDs) != 2 {
		t.Errorf("member role count = %d, want 2", len(member.RoleIDs))
	}
	if !member.Meta.OccurredAt.Equal(memberChanged) {
		t.Errorf("member occurred_at = %s (%d), want %s (%d)",
			member.Meta.OccurredAt, member.Meta.OccurredAt.UnixNano(), memberChanged, memberChanged.UnixNano())
	}
	overrides := overridesByChannelID[voiceID.String()]
	if overrides.ChannelID != voiceID.String() {
		t.Errorf("override channel id = %s, want %s", overrides.ChannelID, voiceID)
	}
	if len(overrides.Overrides) != 1 {
		t.Errorf("override count = %d, want 1", len(overrides.Overrides))
	}
	if !overrides.Meta.OccurredAt.Equal(overrideChanged) {
		t.Errorf("override occurred_at = %s (%d), want %s (%d)",
			overrides.Meta.OccurredAt, overrides.Meta.OccurredAt.UnixNano(), overrideChanged, overrideChanged.UnixNano())
	}
	emptyOverrides := overridesByChannelID[emptyVoiceID.String()]
	if emptyOverrides.ChannelID != emptyVoiceID.String() {
		t.Errorf("empty override channel id = %s, want %s", emptyOverrides.ChannelID, emptyVoiceID)
	}
	if len(emptyOverrides.Overrides) != 0 {
		t.Errorf("empty override count = %d, want 0", len(emptyOverrides.Overrides))
	}
	if !emptyOverrides.Meta.OccurredAt.Equal(base) {
		t.Errorf("empty override occurred_at = %s (%d), want %s (%d)",
			emptyOverrides.Meta.OccurredAt, emptyOverrides.Meta.OccurredAt.UnixNano(), base, base.UnixNano())
	}
	if mute.UserID != userID {
		t.Errorf("mute user id = %s, want %s", mute.UserID, userID)
	}
	if !mute.UntilAt.Equal(muteUntil) {
		t.Errorf("mute until_at = %s (%d), want %s (%d)",
			mute.UntilAt, mute.UntilAt.UnixNano(), muteUntil, muteUntil.UnixNano())
	}
	if !mute.Meta.OccurredAt.Equal(muteChanged) {
		t.Errorf("mute occurred_at = %s (%d), want %s (%d)",
			mute.Meta.OccurredAt, mute.Meta.OccurredAt.UnixNano(), muteChanged, muteChanged.UnixNano())
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
		"0011_outbox_events",
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
