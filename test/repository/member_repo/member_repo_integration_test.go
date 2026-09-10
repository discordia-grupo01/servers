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
	"github.com/discordia-grupo01/servers/internal/repository"
	"github.com/discordia-grupo01/servers/internal/repository/member_repo"
	"github.com/discordia-grupo01/servers/internal/repository/role_repo"
)

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
		"../../../migrations/0001_init.sql",
		"../../../migrations/0002_roles.sql",
		"../../../migrations/0003_member_roles.sql",
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
	if err := servers.CreateServer(ctx, srv, nil); err != nil {
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
	if err := roles.Create(ctx, rl); err != nil {
		t.Fatalf("seed role: %v", err)
	}
	return rl
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

// dropAllTestTables wipes every table any of this repo's integration test
// packages might have created, so a test run leaves the database exactly as
// clean as it found it -- both before it starts and after it finishes.
func dropAllTestTables(ctx context.Context, pool *pgxpool.Pool) {
	_, _ = pool.Exec(ctx, `DROP TABLE IF EXISTS member_roles, roles, server_members, channels, servers CASCADE`)
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
