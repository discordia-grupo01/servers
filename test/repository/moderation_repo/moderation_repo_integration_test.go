package moderation_repo_test

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/model/moderation"
	"github.com/discordia-grupo01/servers/internal/model/role"
	"github.com/discordia-grupo01/servers/internal/model/userprofile"
	"github.com/discordia-grupo01/servers/internal/outbox"
	"github.com/discordia-grupo01/servers/internal/repository"
	"github.com/discordia-grupo01/servers/internal/repository/moderation_repo"
)

func newRepo(t *testing.T) (*moderation_repo.Repository, *pgxpool.Pool) {
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
	t.Cleanup(func() { dropAllTestTables(ctx, pool) })

	dropAllTestTables(ctx, pool)
	for _, file := range []string{
		"../../../migrations/0001_init.up.sql",
		"../../../migrations/0002_invitations.up.sql",
		"../../../migrations/0003_roles.up.sql",
		"../../../migrations/0004_member_roles.up.sql",
		"../../../migrations/0006_ownership_transfers.up.sql",
		"../../../migrations/0007_categories.up.sql",
		"../../../migrations/0008_ownership_transfer_cancelled_status.up.sql",
		"../../../migrations/0011_outbox_events.up.sql",
		"../../../migrations/0015_role_positions.up.sql",
		"../../../migrations/0016_server_bans_banned_by.up.sql",
		"../../../migrations/0017_user_profiles.up.sql",
		"../../../migrations/0019_everyone_role.up.sql",
		"../../../migrations/0020_channel_permission_overrides.up.sql",
		"../../../migrations/0021_server_mutes.up.sql",
	} {
		schema, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read migration %s: %v", file, err)
		}
		if _, err := pool.Exec(ctx, string(schema)); err != nil {
			t.Fatalf("apply migration %s: %v", file, err)
		}
	}
	return moderation_repo.NewRepository(pool), pool
}

func dropAllTestTables(ctx context.Context, pool *pgxpool.Pool) {
	_, _ = pool.Exec(ctx, `DROP TABLE IF EXISTS channel_permission_overrides, categories, user_profiles, outbox_events, ownership_transfers, member_roles, roles, server_mutes, server_bans, invitations, server_members, channels, servers CASCADE`)
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func exec(ctx context.Context, t *testing.T, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(ctx, sql, args...); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
}

func count(ctx context.Context, t *testing.T, pool *pgxpool.Pool, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx, sql, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
	return n
}

const owner = "owner-1"

// seedServer creates a server owned by owner with the given extra members.
func seedServer(ctx context.Context, t *testing.T, pool *pgxpool.Pool, members ...string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	exec(ctx, t, pool, `INSERT INTO servers (id, name, name_key, owner_id) VALUES ($1, 'Server', $2, $3)`, id, "s-"+id.String(), owner)
	exec(ctx, t, pool, `INSERT INTO server_members (server_id, user_id, is_owner) VALUES ($1, $2, TRUE)`, id, owner)
	for _, m := range members {
		exec(ctx, t, pool, `INSERT INTO server_members (server_id, user_id) VALUES ($1, $2)`, id, m)
	}
	return id
}

func events() []outbox.Event {
	return []outbox.Event{
		{RoutingKey: "servers.member_left", Payload: []byte(`{"n":1}`)},
		{RoutingKey: "servers.member_banned", Payload: []byte(`{"n":2}`)},
	}
}

// allowAll is a check that lets every ban through, for tests about what the
// transaction writes rather than about who may ban.
func allowAll(_, _ role.Standing) error { return nil }

func newBan(serverID uuid.UUID, userID string) moderation.Ban {
	reason, by := "spam", "mod-1"
	return moderation.Ban{ServerID: serverID, UserID: userID, Reason: &reason, BannedBy: &by, BannedAt: time.Now().UTC().Truncate(time.Millisecond)}
}

func muteEvent(routingKey string) outbox.Event {
	return outbox.Event{RoutingKey: routingKey, Payload: []byte(`{"event_id":"mute-event"}`)}
}

func newMute(serverID uuid.UUID, userID string, mutedAt, untilAt time.Time) moderation.Mute {
	return moderation.Mute{
		ServerID: serverID,
		UserID:   userID,
		MutedBy:  "mod-1",
		MutedAt:  mutedAt,
		UntilAt:  untilAt,
		Reason:   "spam",
	}
}

func TestMuteMember_PersistsMuteAndEventAtomically(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	serverID := seedServer(ctx, t, pool, "target")
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	mute := newMute(serverID, "target", now, now.Add(time.Hour))

	if err := repo.MuteMember(ctx, mute, allowAll, muteEvent("servers.member_muted")); err != nil {
		t.Fatalf("MuteMember: %v", err)
	}

	var got moderation.Mute
	if err := pool.QueryRow(ctx, `
		SELECT server_id, user_id, muted_by, muted_at, until_at, reason
		FROM server_mutes WHERE server_id = $1 AND user_id = $2`, serverID, "target").
		Scan(&got.ServerID, &got.UserID, &got.MutedBy, &got.MutedAt, &got.UntilAt, &got.Reason); err != nil {
		t.Fatalf("mute row: %v", err)
	}
	if got.ServerID != mute.ServerID || got.UserID != mute.UserID || got.MutedBy != mute.MutedBy ||
		!got.MutedAt.Equal(mute.MutedAt) || !got.UntilAt.Equal(mute.UntilAt) || got.Reason != mute.Reason {
		t.Errorf("mute row = %+v, want %+v", got, mute)
	}
	if n := count(ctx, t, pool, `SELECT COUNT(*) FROM outbox_events WHERE routing_key = 'servers.member_muted'`); n != 1 {
		t.Errorf("mute events = %d, want 1", n)
	}
}

func TestUnmuteMember_DeletesMuteAndWritesEventAtomically(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	serverID := seedServer(ctx, t, pool, "target")
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	exec(ctx, t, pool, `
		INSERT INTO server_mutes (server_id, user_id, muted_by, muted_at, until_at, reason)
		VALUES ($1, 'target', 'mod-1', $2, $3, 'spam')`, serverID, now, now.Add(time.Hour))

	if err := repo.UnmuteMember(ctx, serverID, "target", muteEvent("servers.member_unmuted")); err != nil {
		t.Fatalf("UnmuteMember: %v", err)
	}
	if n := count(ctx, t, pool, `SELECT COUNT(*) FROM server_mutes WHERE server_id = $1`, serverID); n != 0 {
		t.Errorf("mute rows = %d, want 0", n)
	}
	if n := count(ctx, t, pool, `SELECT COUNT(*) FROM outbox_events WHERE routing_key = 'servers.member_unmuted'`); n != 1 {
		t.Errorf("unmute events = %d, want 1", n)
	}
}

func TestExpireMutes_RemovesOnlyExpiredMutesAndPublishesEvents(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	serverID := seedServer(ctx, t, pool, "expired", "active")
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	exec(ctx, t, pool, `
		INSERT INTO server_mutes (server_id, user_id, muted_by, muted_at, until_at, reason)
		VALUES
			($1, 'expired', 'mod-1', $2, $3, 'expired'),
			($1, 'active', 'mod-1', $2, $4, 'active')`,
		serverID, now.Add(-time.Hour), now.Add(-time.Minute), now.Add(time.Hour))

	countExpired, err := repo.ExpireMutes(ctx, now)
	if err != nil {
		t.Fatalf("ExpireMutes: %v", err)
	}
	if countExpired != 1 {
		t.Fatalf("expired count = %d, want 1", countExpired)
	}
	if n := count(ctx, t, pool, `SELECT COUNT(*) FROM server_mutes WHERE server_id = $1 AND user_id = 'expired'`, serverID); n != 0 {
		t.Error("expired mute remains")
	}
	if n := count(ctx, t, pool, `SELECT COUNT(*) FROM server_mutes WHERE server_id = $1 AND user_id = 'active'`, serverID); n != 1 {
		t.Error("active mute was removed")
	}
	if n := count(ctx, t, pool, `SELECT COUNT(*) FROM outbox_events WHERE routing_key = 'servers.member_unmuted'`); n != 1 {
		t.Errorf("expiry events = %d, want 1", n)
	}
}

// CA1: the member is removed with their roles, the ban is recorded, a pending
// transfer to them is cancelled and both events land in the outbox -- all in
// one transaction.
func TestBanMember_RemovesMemberRecordsBanCancelsTransferAndWritesEvents(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	serverID := seedServer(ctx, t, pool, "target", "bystander")

	roleID := uuid.New()
	exec(ctx, t, pool, `INSERT INTO roles (id, server_id, name, color, position) VALUES ($1, $2, 'R', '#000000', 1)`, roleID, serverID)
	exec(ctx, t, pool, `INSERT INTO member_roles (server_id, user_id, role_id) VALUES ($1, 'target', $2)`, serverID, roleID)
	transferID := uuid.New()
	exec(ctx, t, pool, `INSERT INTO ownership_transfers (id, server_id, from_user_id, to_user_id) VALUES ($1, $2, $3, 'target')`, transferID, serverID, owner)

	ban := newBan(serverID, "target")
	if err := repo.BanMember(ctx, ban, allowAll, events()...); err != nil {
		t.Fatalf("BanMember: %v", err)
	}

	if n := count(ctx, t, pool, `SELECT COUNT(*) FROM server_members WHERE server_id = $1 AND user_id = 'target'`, serverID); n != 0 {
		t.Error("the target is still a member")
	}
	if n := count(ctx, t, pool, `SELECT COUNT(*) FROM member_roles WHERE server_id = $1 AND user_id = 'target'`, serverID); n != 0 {
		t.Error("the target kept their role assignments")
	}
	if n := count(ctx, t, pool, `SELECT COUNT(*) FROM server_members WHERE server_id = $1 AND user_id = 'bystander'`, serverID); n != 1 {
		t.Error("another member was removed")
	}

	var reason, bannedBy string
	var bannedAt time.Time
	if err := pool.QueryRow(ctx, `SELECT reason, banned_by, banned_at FROM server_bans WHERE server_id = $1 AND user_id = 'target'`, serverID).
		Scan(&reason, &bannedBy, &bannedAt); err != nil {
		t.Fatalf("ban row: %v", err)
	}
	if reason != "spam" || bannedBy != "mod-1" || !bannedAt.Equal(ban.BannedAt) {
		t.Errorf("ban row = %q, %q, %v", reason, bannedBy, bannedAt)
	}

	var status string
	var resolved *time.Time
	if err := pool.QueryRow(ctx, `SELECT status, resolved_at FROM ownership_transfers WHERE id = $1`, transferID).Scan(&status, &resolved); err != nil {
		t.Fatal(err)
	}
	if status != "cancelled" || resolved == nil {
		t.Errorf("transfer = %s, resolved_at %v; want cancelled with a timestamp", status, resolved)
	}

	if n := count(ctx, t, pool, `SELECT COUNT(*) FROM outbox_events WHERE routing_key IN ('servers.member_left', 'servers.member_banned')`); n != 2 {
		t.Errorf("outbox has %d ban events, want 2", n)
	}
}

// Not a member: nothing is kept -- no ban row, no events (full rollback).
func TestBanMember_NotAMember_RollsBackEverything(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	serverID := seedServer(ctx, t, pool)

	err := repo.BanMember(ctx, newBan(serverID, "stranger"), allowAll, events()...)
	if !errors.Is(err, model.ErrMemberNotFound) {
		t.Fatalf("err = %v, want ErrMemberNotFound", err)
	}
	if n := count(ctx, t, pool, `SELECT COUNT(*) FROM server_bans`); n != 0 {
		t.Error("a ban was kept after the rollback")
	}
	if n := count(ctx, t, pool, `SELECT COUNT(*) FROM outbox_events`); n != 0 {
		t.Error("events were kept after the rollback")
	}
}

func TestBanMember_AlreadyBanned_Conflict(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	serverID := seedServer(ctx, t, pool)
	exec(ctx, t, pool, `INSERT INTO server_bans (server_id, user_id) VALUES ($1, 'banned')`, serverID)

	err := repo.BanMember(ctx, newBan(serverID, "banned"), allowAll, events()...)
	if !errors.Is(err, model.ErrAlreadyBanned) {
		t.Fatalf("err = %v, want ErrAlreadyBanned", err)
	}
	if n := count(ctx, t, pool, `SELECT COUNT(*) FROM outbox_events`); n != 0 {
		t.Error("events written for a duplicate ban")
	}
}

// The owner check runs under the server lock, so it holds even if ownership
// changed after the service looked at the server.
func TestBanMember_TargetIsTheOwner(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	serverID := seedServer(ctx, t, pool, "target")
	exec(ctx, t, pool, `UPDATE servers SET owner_id = 'target' WHERE id = $1`, serverID)

	err := repo.BanMember(ctx, newBan(serverID, "target"), allowAll, events()...)
	if !errors.Is(err, model.ErrCannotBanOwner) {
		t.Fatalf("err = %v, want ErrCannotBanOwner", err)
	}
	if n := count(ctx, t, pool, `SELECT COUNT(*) FROM server_members WHERE user_id = 'target'`); n != 1 {
		t.Error("the owner was removed")
	}
}

func TestBanMember_ServerNotFound(t *testing.T) {
	repo, _ := newRepo(t)
	err := repo.BanMember(context.Background(), newBan(uuid.New(), "x"), allowAll, events()...)
	if !errors.Is(err, model.ErrServerNotFound) {
		t.Fatalf("err = %v, want ErrServerNotFound", err)
	}
}

// Two moderators banning the same member at once: exactly one wins and the
// events are written once.
func TestBanMember_ConcurrentBans_OnlyOneWins(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	serverID := seedServer(ctx, t, pool, "target")

	var wg sync.WaitGroup
	errs := make([]error, 5)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = repo.BanMember(ctx, newBan(serverID, "target"), allowAll, events()...)
		}(i)
	}
	wg.Wait()

	wins := 0
	for _, err := range errs {
		switch {
		case err == nil:
			wins++
		case errors.Is(err, model.ErrAlreadyBanned):
		default:
			t.Errorf("unexpected error: %v", err)
		}
	}
	if wins != 1 {
		t.Errorf("%d bans succeeded, want exactly 1", wins)
	}
	if n := count(ctx, t, pool, `SELECT COUNT(*) FROM outbox_events`); n != 2 {
		t.Errorf("outbox has %d events, want 2", n)
	}
}

func unbannedEvent() outbox.Event {
	return outbox.Event{RoutingKey: "servers.member_unbanned", Payload: []byte(`{"n":3}`)}
}

// #26 CA1: the ban row goes away and the event lands in the outbox, in the
// same transaction. The user is not made a member again.
func TestUnbanMember_DeletesBanAndWritesEvent(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	serverID := seedServer(ctx, t, pool, "target")
	if err := repo.BanMember(ctx, newBan(serverID, "target"), allowAll); err != nil {
		t.Fatalf("BanMember: %v", err)
	}

	if err := repo.UnbanMember(ctx, serverID, "target", unbannedEvent()); err != nil {
		t.Fatalf("UnbanMember: %v", err)
	}
	if n := count(ctx, t, pool, `SELECT COUNT(*) FROM server_bans WHERE server_id = $1`, serverID); n != 0 {
		t.Errorf("%d bans left, want 0", n)
	}
	if n := count(ctx, t, pool, `SELECT COUNT(*) FROM server_members WHERE server_id = $1 AND user_id = 'target'`, serverID); n != 0 {
		t.Error("the unban made the user a member again")
	}
	if n := count(ctx, t, pool, `SELECT COUNT(*) FROM outbox_events WHERE routing_key = 'servers.member_unbanned'`); n != 1 {
		t.Errorf("outbox has %d member_unbanned events, want 1", n)
	}
}

func TestUnbanMember_NotBanned_NotFoundAndNoEvent(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	serverID := seedServer(ctx, t, pool, "member")
	// Banned on another server, which doesn't count.
	other := seedServer(ctx, t, pool)
	exec(ctx, t, pool, `INSERT INTO server_bans (server_id, user_id) VALUES ($1, 'member')`, other)

	for _, user := range []string{"member", "stranger"} {
		if err := repo.UnbanMember(ctx, serverID, user, unbannedEvent()); !errors.Is(err, model.ErrBanNotFound) {
			t.Errorf("%s: err = %v, want ErrBanNotFound", user, err)
		}
	}
	if n := count(ctx, t, pool, `SELECT COUNT(*) FROM server_bans WHERE server_id = $1`, other); n != 1 {
		t.Error("the ban on the other server was touched")
	}
	if n := count(ctx, t, pool, `SELECT COUNT(*) FROM outbox_events`); n != 0 {
		t.Errorf("outbox has %d events, want 0", n)
	}
}

// Two moderators revoking the same ban at once: one wins, the others get
// ErrBanNotFound, and the event is written once.
func TestUnbanMember_ConcurrentUnbans_OnlyOneWins(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	serverID := seedServer(ctx, t, pool)
	exec(ctx, t, pool, `INSERT INTO server_bans (server_id, user_id) VALUES ($1, 'target')`, serverID)

	var wg sync.WaitGroup
	errs := make([]error, 5)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = repo.UnbanMember(ctx, serverID, "target", unbannedEvent())
		}(i)
	}
	wg.Wait()

	wins := 0
	for _, err := range errs {
		switch {
		case err == nil:
			wins++
		case errors.Is(err, model.ErrBanNotFound):
		default:
			t.Errorf("unexpected error: %v", err)
		}
	}
	if wins != 1 {
		t.Errorf("%d unbans succeeded, want exactly 1", wins)
	}
	if n := count(ctx, t, pool, `SELECT COUNT(*) FROM outbox_events`); n != 1 {
		t.Errorf("outbox has %d events, want 1", n)
	}
}

func TestListBans_NewestFirstPaginatedAndScopedToTheServer(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	serverID := seedServer(ctx, t, pool)
	other := seedServer(ctx, t, pool)
	base := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	for i, u := range []string{"oldest", "middle", "newest"} {
		exec(ctx, t, pool, `INSERT INTO server_bans (server_id, user_id, banned_by, banned_at) VALUES ($1, $2, 'mod-1', $3)`,
			serverID, u, base.Add(time.Duration(i)*time.Hour))
	}
	// A ban from before banned_by existed.
	exec(ctx, t, pool, `INSERT INTO server_bans (server_id, user_id, banned_at) VALUES ($1, 'legacy', $2)`, serverID, base.Add(-time.Hour))
	exec(ctx, t, pool, `INSERT INTO server_bans (server_id, user_id) VALUES ($1, 'elsewhere')`, other)

	bans, total, err := repo.ListBans(ctx, serverID, 2, 0)
	if err != nil {
		t.Fatalf("ListBans: %v", err)
	}
	if total != 4 || len(bans) != 2 || bans[0].UserID != "newest" || bans[1].UserID != "middle" {
		t.Errorf("page 1 = %v (total %d)", userIDs(bans), total)
	}
	if bans[0].BannedBy == nil || *bans[0].BannedBy != "mod-1" {
		t.Errorf("banned_by = %v", bans[0].BannedBy)
	}

	bans, _, err = repo.ListBans(ctx, serverID, 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(bans) != 2 || bans[0].UserID != "oldest" || bans[1].UserID != "legacy" || bans[1].BannedBy != nil {
		t.Errorf("page 2 = %v", userIDs(bans))
	}
}

// A banned user is no longer a member, so ListBans is the only place their
// replicated profile can come from. A user whose profile hasn't arrived yet
// still shows up, with a nil Profile, instead of being dropped from the page.
func TestListBans_JoinsReplicatedProfiles(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	serverID := seedServer(ctx, t, pool)
	base := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	exec(ctx, t, pool, `INSERT INTO server_bans (server_id, user_id, banned_at) VALUES ($1, 'with-profile', $2), ($1, 'no-profile', $3)`,
		serverID, base.Add(time.Hour), base)
	exec(ctx, t, pool, `
		INSERT INTO user_profiles (user_id, name, avatar_url, description, status_text, status_emoji, version)
		VALUES ('with-profile', 'Manu', 'https://cdn/a.png', 'hola', 'jugando', '🎮', 4)`)

	bans, total, err := repo.ListBans(ctx, serverID, 10, 0)
	if err != nil {
		t.Fatalf("ListBans: %v", err)
	}
	if total != 2 || len(bans) != 2 {
		t.Fatalf("total = %d, len = %d; want 2, 2", total, len(bans))
	}
	want := userprofile.Profile{UserID: "with-profile", Name: "Manu", AvatarURL: "https://cdn/a.png", Description: "hola", StatusText: "jugando", StatusEmoji: "🎮", Version: 4}
	if bans[0].UserID != "with-profile" || bans[0].Profile == nil || *bans[0].Profile != want {
		t.Errorf("first ban = %+v, want profile %+v", bans[0], want)
	}
	if bans[1].UserID != "no-profile" || bans[1].Profile != nil {
		t.Errorf("second ban = %+v, want nil profile", bans[1])
	}
}

func TestListBans_NoBans_EmptyNotNil(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	bans, total, err := repo.ListBans(ctx, seedServer(ctx, t, pool), 20, 0)
	if err != nil || total != 0 || bans == nil || len(bans) != 0 {
		t.Fatalf("got %v, %d, %v", bans, total, err)
	}
}

func userIDs(bans []moderation.Ban) []string {
	out := make([]string, len(bans))
	for i, b := range bans {
		out[i] = b.UserID
	}
	return out
}

// seedRoleFor gives userID a role at the given position with perms.
func seedRoleFor(ctx context.Context, t *testing.T, pool *pgxpool.Pool, serverID uuid.UUID, userID string, position int, perms role.Permission) {
	t.Helper()
	id := uuid.New()
	exec(ctx, t, pool, `INSERT INTO roles (id, server_id, name, color, permissions, position) VALUES ($1, $2, 'R', '#FFFFFF', $3, $4)`, id, serverID, perms, position)
	exec(ctx, t, pool, `INSERT INTO member_roles (server_id, user_id, role_id) VALUES ($1, $2, $3)`, serverID, userID, id)
}

// #25 CA2 at write time: the check gets the actor's and the target's
// standings read inside the transaction, under the server lock -- here, what
// the database says right now, including the owner.
func TestBanMember_CheckGetsTheStandingsReadUnderTheLock(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	serverID := seedServer(ctx, t, pool, "mod-1", "target")
	seedRoleFor(ctx, t, pool, serverID, "mod-1", 1, role.PermBanMembers)
	seedRoleFor(ctx, t, pool, serverID, "target", 2, role.PermSendMessages)

	var gotActor, gotTarget role.Standing
	err := repo.BanMember(ctx, newBan(serverID, "target"), func(actor, target role.Standing) error {
		gotActor, gotTarget = actor, target
		return nil
	})
	if err != nil {
		t.Fatalf("BanMember: %v", err)
	}
	if !gotActor.IsMember || gotActor.Permissions != role.PermBanMembers || gotActor.TopPosition == nil || *gotActor.TopPosition != 1 {
		t.Errorf("actor = %+v", gotActor)
	}
	if !gotTarget.IsMember || gotTarget.TopPosition == nil || *gotTarget.TopPosition != 2 {
		t.Errorf("target = %+v", gotTarget)
	}

	// The owner is resolved from the locked server row.
	other := seedServer(ctx, t, pool, "target")
	ownerBan := newBan(other, "target")
	by := owner
	ownerBan.BannedBy = &by
	if err := repo.BanMember(ctx, ownerBan, func(actor, _ role.Standing) error {
		gotActor = actor
		return nil
	}); err != nil {
		t.Fatalf("BanMember by owner: %v", err)
	}
	if !gotActor.IsOwner {
		t.Errorf("owner's standing = %+v, want IsOwner", gotActor)
	}
}

// If the check rejects the ban (e.g. the target was just given a higher
// role), its error comes back as-is and nothing is written.
func TestBanMember_CheckRejects_WritesNothing(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	serverID := seedServer(ctx, t, pool, "target")

	err := repo.BanMember(ctx, newBan(serverID, "target"), func(_, _ role.Standing) error {
		return model.ErrTargetOutranksActor
	}, events()...)
	if !errors.Is(err, model.ErrTargetOutranksActor) {
		t.Fatalf("err = %v, want ErrTargetOutranksActor", err)
	}
	if n := count(ctx, t, pool, `SELECT COUNT(*) FROM server_bans`); n != 0 {
		t.Errorf("%d bans written", n)
	}
	if n := count(ctx, t, pool, `SELECT COUNT(*) FROM server_members WHERE server_id = $1 AND user_id = 'target'`, serverID); n != 1 {
		t.Error("the member was removed")
	}
	if n := count(ctx, t, pool, `SELECT COUNT(*) FROM outbox_events`); n != 0 {
		t.Errorf("outbox has %d events", n)
	}
}
