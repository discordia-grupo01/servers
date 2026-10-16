package member_repo_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/model/role"
	"github.com/discordia-grupo01/servers/internal/model/userprofile"
	"github.com/discordia-grupo01/servers/internal/outbox"
	"github.com/discordia-grupo01/servers/internal/repository"
	"github.com/discordia-grupo01/servers/internal/repository/member_repo"
	"github.com/discordia-grupo01/servers/internal/repository/role_repo"
)

// testOutboxEvent is a valid placeholder for CreateServer/RemoveMember's
// outbox event in tests that don't care about its content, only that the
// call as a whole succeeds -- payload can't be nil (outbox_events.payload
// is NOT NULL).
func testOutboxEvent() outbox.Event {
	return outbox.Event{RoutingKey: "test.event", Payload: []byte("{}")}
}

func newRepo(t *testing.T) (*member_repo.MemberRepository, *repository.ServerRepository, *role_repo.RoleRepository, *pgxpool.Pool) {
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
		"../../../migrations/0003_roles.up.sql",
		"../../../migrations/0004_member_roles.up.sql",
		"../../../migrations/0005_default_role.up.sql",
		"../../../migrations/0007_categories.up.sql",
		"../../../migrations/0011_outbox_events.up.sql",
		"../../../migrations/0015_role_positions.up.sql",
		"../../../migrations/0017_user_profiles.up.sql",
		"../../../migrations/0019_everyone_role.up.sql",
		"../../../migrations/0020_channel_permission_overrides.up.sql",
		"../../../migrations/0021_server_mutes.up.sql",
		"../../../migrations/0027_suspended_users.up.sql",
	} {
		schema, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read migration %s: %v", file, err)
		}
		if _, err := pool.Exec(ctx, string(schema)); err != nil {
			t.Fatalf("apply migration %s: %v", file, err)
		}
	}
	return member_repo.NewMemberRepository(pool), repository.NewServerRepository(pool), role_repo.NewRoleRepository(pool), pool
}

func seedServerWithMember(ctx context.Context, t *testing.T, servers *repository.ServerRepository, pool *pgxpool.Pool) (model.Server, string) {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Millisecond)
	srv := model.Server{ID: uuid.New(), Name: "Test Server", NameKey: "test server", OwnerID: "owner-" + uuid.NewString(), CreatedAt: now}
	if err := servers.CreateServer(ctx, srv, nil, nil, []outbox.Event{testOutboxEvent()}); err != nil {
		t.Fatalf("seed server: %v", err)
	}

	member := "member-" + uuid.NewString()
	if _, err := pool.Exec(ctx,
		`INSERT INTO server_members (server_id, user_id, is_owner, joined_at) VALUES ($1, $2, FALSE, $3)`,
		srv.ID, member, now,
	); err != nil {
		t.Fatalf("seed member: %v", err)
	}
	return srv, member
}

func seedRole(ctx context.Context, t *testing.T, roles *role_repo.RoleRepository, serverID uuid.UUID, name string) role.Role {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Millisecond)
	rl := role.Role{ID: uuid.New(), ServerID: serverID, Name: name, Color: "#FF00AA", CreatedAt: now, UpdatedAt: now}
	created, err := roles.Create(ctx, rl)
	if err != nil {
		t.Fatalf("seed role: %v", err)
	}
	return created
}

func TestMemberRepo_IsMember_TrueForAMemberFalseOtherwise(t *testing.T) {
	members, servers, _, pool := newRepo(t)
	ctx := context.Background()
	srv, member := seedServerWithMember(ctx, t, servers, pool)

	isMember, err := members.IsMember(ctx, srv.ID, member)
	if err != nil || !isMember {
		t.Errorf("IsMember(member) = %v, %v; want true, nil", isMember, err)
	}

	isMember, err = members.IsMember(ctx, srv.ID, "not-a-member")
	if err != nil || isMember {
		t.Errorf("IsMember(stranger) = %v, %v; want false, nil", isMember, err)
	}
}

func TestMemberRepo_AssignRole_ThenListRolesForMember_ReturnsIt(t *testing.T) {
	members, servers, roles, pool := newRepo(t)
	ctx := context.Background()
	srv, member := seedServerWithMember(ctx, t, servers, pool)
	rl := seedRole(ctx, t, roles, srv.ID, "Moderator")

	if err := members.AssignRole(ctx, srv.ID, member, rl.ID); err != nil {
		t.Fatalf("AssignRole: %v", err)
	}

	got, err := members.ListRolesForMember(ctx, srv.ID, member)
	if err != nil {
		t.Fatalf("ListRolesForMember: %v", err)
	}
	if len(got) != 1 || got[0].ID != rl.ID {
		t.Errorf("got = %+v, want [%v]", got, rl.ID)
	}
}

func TestMemberRepo_AssignRole_Idempotent_NoDuplicateNoError(t *testing.T) {
	members, servers, roles, pool := newRepo(t)
	ctx := context.Background()
	srv, member := seedServerWithMember(ctx, t, servers, pool)
	rl := seedRole(ctx, t, roles, srv.ID, "Moderator")

	if err := members.AssignRole(ctx, srv.ID, member, rl.ID); err != nil {
		t.Fatalf("first AssignRole: %v", err)
	}
	if err := members.AssignRole(ctx, srv.ID, member, rl.ID); err != nil {
		t.Fatalf("second AssignRole (duplicate) should be a no-op, got: %v", err)
	}

	got, err := members.ListRolesForMember(ctx, srv.ID, member)
	if err != nil {
		t.Fatalf("ListRolesForMember: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("got %d roles, want exactly 1 (no duplicate row)", len(got))
	}
}

func TestMemberRepo_RemoveRole_ThenListIsEmpty(t *testing.T) {
	members, servers, roles, pool := newRepo(t)
	ctx := context.Background()
	srv, member := seedServerWithMember(ctx, t, servers, pool)
	rl := seedRole(ctx, t, roles, srv.ID, "Moderator")

	if err := members.AssignRole(ctx, srv.ID, member, rl.ID); err != nil {
		t.Fatalf("AssignRole: %v", err)
	}
	if err := members.RemoveRole(ctx, srv.ID, member, rl.ID); err != nil {
		t.Fatalf("RemoveRole: %v", err)
	}

	got, err := members.ListRolesForMember(ctx, srv.ID, member)
	if err != nil {
		t.Fatalf("ListRolesForMember: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d roles, want 0 after removal", len(got))
	}
}

func TestMemberRepo_ListRolesForMember_ReportsIsEveryone(t *testing.T) {
	members, servers, roles, pool := newRepo(t)
	ctx := context.Background()
	srv, member := seedServerWithMember(ctx, t, servers, pool)
	mod := seedRole(ctx, t, roles, srv.ID, "Moderator")

	all, err := roles.ListByServer(ctx, srv.ID)
	if err != nil {
		t.Fatalf("ListByServer: %v", err)
	}
	var everyone role.Role
	found := false
	for _, rl := range all {
		if rl.IsEveryone {
			everyone = rl
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("server %v has no @everyone role; CreateServer should always seed one", srv.ID)
	}

	for _, id := range []uuid.UUID{everyone.ID, mod.ID} {
		if err := members.AssignRole(ctx, srv.ID, member, id); err != nil {
			t.Fatalf("AssignRole(%v): %v", id, err)
		}
	}

	got, err := members.ListRolesForMember(ctx, srv.ID, member)
	if err != nil {
		t.Fatalf("ListRolesForMember: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d roles, want 2", len(got))
	}

	isEveryoneByID := make(map[uuid.UUID]bool, len(got))
	for _, rl := range got {
		isEveryoneByID[rl.ID] = rl.IsEveryone
	}
	if !isEveryoneByID[everyone.ID] {
		t.Errorf("IsEveryone for the @everyone role (%v) = false, want true -- "+
			"this is exactly the bug that let @everyone show up as a normal, "+
			"removable badge on a member's profile", everyone.ID)
	}
	if isEveryoneByID[mod.ID] {
		t.Errorf("IsEveryone for the Moderator role (%v) = true, want false", mod.ID)
	}
}

func TestMemberRepo_RemoveRole_NotAssigned_ReturnsErrMemberRoleNotFound(t *testing.T) {
	members, servers, roles, pool := newRepo(t)
	ctx := context.Background()
	srv, member := seedServerWithMember(ctx, t, servers, pool)
	rl := seedRole(ctx, t, roles, srv.ID, "Moderator")

	err := members.RemoveRole(ctx, srv.ID, member, rl.ID)
	if !errors.Is(err, model.ErrMemberRoleNotFound) {
		t.Errorf("err = %v, want ErrMemberRoleNotFound", err)
	}
}

func TestMemberRepo_AssignRole_ConcurrentAssignment_NoConflict(t *testing.T) {
	members, servers, roles, pool := newRepo(t)
	ctx := context.Background()
	srv, member := seedServerWithMember(ctx, t, servers, pool)
	rl := seedRole(ctx, t, roles, srv.ID, "Moderator")

	const attempts = 10
	errCh := make(chan error, attempts)
	for i := 0; i < attempts; i++ {
		go func() {
			errCh <- members.AssignRole(ctx, srv.ID, member, rl.ID)
		}()
	}
	for i := 0; i < attempts; i++ {
		if err := <-errCh; err != nil {
			t.Errorf("concurrent AssignRole #%d failed: %v", i, err)
		}
	}

	got, err := members.ListRolesForMember(ctx, srv.ID, member)
	if err != nil {
		t.Fatalf("ListRolesForMember: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("got %d roles after concurrent assignment, want exactly 1 (no duplicate)", len(got))
	}
}

func TestMemberRepo_RemoveMember_ThenIsMemberFalse(t *testing.T) {
	members, servers, _, pool := newRepo(t)
	ctx := context.Background()
	srv, member := seedServerWithMember(ctx, t, servers, pool)

	removed, err := members.RemoveMember(ctx, srv.ID, member, testOutboxEvent())
	if err != nil || !removed {
		t.Fatalf("RemoveMember = %v, %v; want true, nil", removed, err)
	}

	isMember, err := members.IsMember(ctx, srv.ID, member)
	if err != nil || isMember {
		t.Errorf("IsMember after RemoveMember = %v, %v; want false, nil", isMember, err)
	}
}

func TestMemberRepo_RemoveMember_NotAMember_ReturnsFalse(t *testing.T) {
	members, servers, _, pool := newRepo(t)
	ctx := context.Background()
	srv, _ := seedServerWithMember(ctx, t, servers, pool)

	removed, err := members.RemoveMember(ctx, srv.ID, "never-joined", testOutboxEvent())
	if err != nil || removed {
		t.Errorf("RemoveMember(non-member) = %v, %v; want false, nil", removed, err)
	}
}

func TestMemberRepo_RemoveMember_CascadesToMemberRoles(t *testing.T) {
	members, servers, roles, pool := newRepo(t)
	ctx := context.Background()
	srv, member := seedServerWithMember(ctx, t, servers, pool)
	rl := seedRole(ctx, t, roles, srv.ID, "Moderator")

	if err := members.AssignRole(ctx, srv.ID, member, rl.ID); err != nil {
		t.Fatalf("AssignRole: %v", err)
	}

	if _, err := members.RemoveMember(ctx, srv.ID, member, testOutboxEvent()); err != nil {
		t.Fatalf("RemoveMember: %v", err)
	}

	var count int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM member_roles WHERE server_id = $1 AND user_id = $2`,
		srv.ID, member,
	).Scan(&count); err != nil {
		t.Fatalf("count member_roles: %v", err)
	}
	if count != 0 {
		t.Errorf("member_roles rows left = %d, want 0 (should cascade on membership delete)", count)
	}
}

func TestMemberRepo_ListMembers_PaginatesAndReportsTotal(t *testing.T) {
	members, servers, _, pool := newRepo(t)
	ctx := context.Background()
	srv, _ := seedServerWithMember(ctx, t, servers, pool)

	// seedServerWithMember already left the server with 2 rows: the owner
	// (inserted by CreateServer) and one plain member. Add one more so
	// paging across 3 members with a page size of 2 is meaningful.
	secondMember := "member-" + uuid.NewString()
	if _, err := pool.Exec(ctx,
		`INSERT INTO server_members (server_id, user_id, is_owner, joined_at) VALUES ($1, $2, FALSE, $3)`,
		srv.ID, secondMember, time.Now().UTC().Add(time.Second),
	); err != nil {
		t.Fatalf("seed second member: %v", err)
	}

	page1, total, err := members.ListMembers(ctx, srv.ID, 2, 0)
	if err != nil {
		t.Fatalf("ListMembers(page1) error = %v", err)
	}
	if total != 3 {
		t.Fatalf("total = %d, want 3", total)
	}
	if len(page1) != 2 {
		t.Fatalf("len(page1) = %d, want 2", len(page1))
	}

	page2, total2, err := members.ListMembers(ctx, srv.ID, 2, 2)
	if err != nil {
		t.Fatalf("ListMembers(page2) error = %v", err)
	}
	if total2 != 3 {
		t.Fatalf("total (page2) = %d, want 3", total2)
	}
	if len(page2) != 1 {
		t.Fatalf("len(page2) = %d, want 1", len(page2))
	}

	seen := map[string]bool{}
	for _, m := range append(page1, page2...) {
		seen[m.UserID] = true
	}
	if len(seen) != 3 {
		t.Errorf("distinct members across pages = %d, want 3 (pages must not overlap)", len(seen))
	}
}

func TestMemberRepo_ListMembers_ReportsOwnerFlag(t *testing.T) {
	members, servers, _, pool := newRepo(t)
	ctx := context.Background()
	srv, member := seedServerWithMember(ctx, t, servers, pool)

	all, _, err := members.ListMembers(ctx, srv.ID, 10, 0)
	if err != nil {
		t.Fatalf("ListMembers error = %v", err)
	}
	byUser := map[string]bool{}
	for _, m := range all {
		byUser[m.UserID] = m.IsOwner
	}
	if isOwner, ok := byUser[srv.OwnerID]; !ok || !isOwner {
		t.Errorf("owner %q is_owner = %v, ok = %v; want true, true", srv.OwnerID, isOwner, ok)
	}
	if isOwner, ok := byUser[member]; !ok || isOwner {
		t.Errorf("member %q is_owner = %v, ok = %v; want false, true", member, isOwner, ok)
	}
}

// dropAllTestTables wipes every table any of this repo's integration test
// packages might have created, so a test run leaves the database exactly as
// clean as it found it -- both before it starts and after it finishes.
func dropAllTestTables(ctx context.Context, pool *pgxpool.Pool) {
	_, _ = pool.Exec(ctx, `DROP TABLE IF EXISTS channel_permission_overrides, categories, user_profiles, suspended_users, outbox_events, member_roles, roles, server_mutes, server_bans, invitations, server_members, channels, servers CASCADE`)
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// Members whose profile has been replicated carry it; the rest get a nil
// Profile instead of being dropped from the page.
func TestMemberRepo_ListMembers_JoinsReplicatedProfiles(t *testing.T) {
	members, servers, _, pool := newRepo(t)
	ctx := context.Background()
	srv, member := seedServerWithMember(ctx, t, servers, pool)

	if _, err := pool.Exec(ctx, `
		INSERT INTO user_profiles (user_id, name, avatar_url, description, status_text, status_emoji, version)
		VALUES ($1, 'Manu', 'https://cdn/a.png', 'hola', 'jugando', '🎮', 4)`, member,
	); err != nil {
		t.Fatalf("seed profile: %v", err)
	}

	all, total, err := members.ListMembers(ctx, srv.ID, 10, 0)
	if err != nil {
		t.Fatalf("ListMembers error = %v", err)
	}
	if total != 2 || len(all) != 2 {
		t.Fatalf("total = %d, len = %d; want 2, 2", total, len(all))
	}
	for _, m := range all {
		switch m.UserID {
		case member:
			if m.Profile == nil {
				t.Fatalf("member %q Profile = nil, want replicated profile", member)
			}
			want := userprofile.Profile{UserID: member, Name: "Manu", AvatarURL: "https://cdn/a.png", Description: "hola", StatusText: "jugando", StatusEmoji: "🎮", Version: 4}
			if *m.Profile != want {
				t.Errorf("Profile = %+v, want %+v", *m.Profile, want)
			}
		case srv.OwnerID:
			if m.Profile != nil {
				t.Errorf("owner Profile = %+v, want nil (not replicated)", *m.Profile)
			}
		}
	}
}

func seedProfile(ctx context.Context, t *testing.T, pool *pgxpool.Pool, user, name string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
		INSERT INTO user_profiles (user_id, name, avatar_url, description, status_text, status_emoji, version)
		VALUES ($1, $2, 'https://cdn/'||$2||'.png', 'hola', 'jugando', 'x', 1)`, user, name,
	); err != nil {
		t.Fatalf("seed profile: %v", err)
	}
}

func setSuspended(ctx context.Context, t *testing.T, pool *pgxpool.Pool, user string, suspended bool, version int64) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
		INSERT INTO suspended_users (user_id, suspended, version) VALUES ($1, $2, $3)
		ON CONFLICT (user_id) DO UPDATE SET suspended = EXCLUDED.suspended, version = EXCLUDED.version`,
		user, suspended, version,
	); err != nil {
		t.Fatalf("set suspended: %v", err)
	}
}

// A suspended member stays in the list (and in the total) but their own name
// and avatar are not returned; everyone else keeps their profile.
func TestMemberRepo_ListMembers_SuspendedMemberHasNoProfile(t *testing.T) {
	members, servers, _, pool := newRepo(t)
	ctx := context.Background()
	srv, member := seedServerWithMember(ctx, t, servers, pool)
	seedProfile(ctx, t, pool, member, "Suspendido")
	seedProfile(ctx, t, pool, srv.OwnerID, "Dueño")
	setSuspended(ctx, t, pool, member, true, 1)

	all, total, err := members.ListMembers(ctx, srv.ID, 10, 0)
	if err != nil {
		t.Fatalf("ListMembers error = %v", err)
	}
	if total != 2 || len(all) != 2 {
		t.Fatalf("total = %d, len = %d; want 2, 2 (the suspended member stays listed)", total, len(all))
	}
	for _, m := range all {
		switch m.UserID {
		case member:
			if m.Profile != nil {
				t.Errorf("suspended member Profile = %+v, want nil", *m.Profile)
			}
		case srv.OwnerID:
			if m.Profile == nil || m.Profile.Name != "Dueño" {
				t.Errorf("owner Profile = %+v, want the replicated one", m.Profile)
			}
		}
	}
}

// After a reactivation the row stays with suspended = FALSE, and the profile
// comes back.
func TestMemberRepo_ListMembers_ReactivatedMemberGetsTheProfileBack(t *testing.T) {
	members, servers, _, pool := newRepo(t)
	ctx := context.Background()
	srv, member := seedServerWithMember(ctx, t, servers, pool)
	seedProfile(ctx, t, pool, member, "Vuelve")
	setSuspended(ctx, t, pool, member, true, 1)
	setSuspended(ctx, t, pool, member, false, 2)

	all, _, err := members.ListMembers(ctx, srv.ID, 10, 0)
	if err != nil {
		t.Fatalf("ListMembers error = %v", err)
	}
	for _, m := range all {
		if m.UserID == member && (m.Profile == nil || m.Profile.Name != "Vuelve") {
			t.Errorf("reactivated member Profile = %+v, want the replicated one", m.Profile)
		}
	}
}

func TestMemberRepo_ListMembers_ReportsOnlyActiveMutes(t *testing.T) {
	members, servers, _, pool := newRepo(t)
	ctx := context.Background()
	srv, muted := seedServerWithMember(ctx, t, servers, pool)
	expired := "expired-" + uuid.NewString()
	if _, err := pool.Exec(ctx,
		`INSERT INTO server_members (server_id, user_id, is_owner, joined_at) VALUES ($1, $2, FALSE, now())`,
		srv.ID, expired,
	); err != nil {
		t.Fatal(err)
	}
	until := time.Now().UTC().Add(time.Hour).Truncate(time.Millisecond)
	for _, mute := range []struct {
		user           string
		mutedAt, until time.Time
	}{
		{muted, time.Now().UTC().Add(-time.Minute), until},
		{expired, time.Now().UTC().Add(-2 * time.Hour), time.Now().UTC().Add(-time.Hour)},
	} {
		if _, err := pool.Exec(ctx,
			`INSERT INTO server_mutes (server_id, user_id, muted_by, muted_at, until_at) VALUES ($1, $2, 'mod', $3, $4)`,
			srv.ID, mute.user, mute.mutedAt, mute.until,
		); err != nil {
			t.Fatal(err)
		}
	}

	list, _, err := members.ListMembers(ctx, srv.ID, 50, 0)
	if err != nil {
		t.Fatalf("ListMembers: %v", err)
	}
	byUser := map[string]model.Member{}
	for _, m := range list {
		byUser[m.UserID] = m
	}
	if got := byUser[muted].MutedUntil; got == nil || !got.Equal(until) {
		t.Errorf("muted member: MutedUntil = %v, want %v", got, until)
	}
	if got := byUser[expired].MutedUntil; got != nil {
		t.Errorf("a member whose mute expired shows as muted until %v", got)
	}
	if got := byUser[srv.OwnerID].MutedUntil; got != nil {
		t.Errorf("a member never muted shows as muted until %v", got)
	}
}

func TestMemberRepo_ListRoleMembers_OnlyWhoHoldsIt(t *testing.T) {
	members, servers, roles, pool := newRepo(t)
	ctx := context.Background()
	srv, holder := seedServerWithMember(ctx, t, servers, pool)
	other := "other-" + uuid.NewString()
	if _, err := pool.Exec(ctx,
		`INSERT INTO server_members (server_id, user_id, is_owner, joined_at) VALUES ($1, $2, FALSE, now())`,
		srv.ID, other,
	); err != nil {
		t.Fatal(err)
	}
	mod := seedRole(ctx, t, roles, srv.ID, "Mod")
	if _, err := pool.Exec(ctx,
		`INSERT INTO member_roles (server_id, user_id, role_id) VALUES ($1, $2, $3)`,
		srv.ID, holder, mod.ID,
	); err != nil {
		t.Fatal(err)
	}

	got, total, err := members.ListRoleMembers(ctx, srv.ID, mod.ID, 50, 0)
	if err != nil {
		t.Fatalf("ListRoleMembers: %v", err)
	}
	if total != 1 || len(got) != 1 || got[0].UserID != holder {
		t.Errorf("got %+v (total %d), want only %s", got, total, holder)
	}

	empty := seedRole(ctx, t, roles, srv.ID, "Nadie")
	got, total, err = members.ListRoleMembers(ctx, srv.ID, empty.ID, 50, 0)
	if err != nil || total != 0 || len(got) != 0 {
		t.Errorf("a role nobody holds: %+v, %d, %v", got, total, err)
	}

	all, allTotal, _ := members.ListMembers(ctx, srv.ID, 50, 0)
	if allTotal != len(all) || allTotal < 3 {
		t.Errorf("the unfiltered list changed: %d members, total %d", len(all), allTotal)
	}
}
