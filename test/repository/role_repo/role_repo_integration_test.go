package role_repo_test

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
	"github.com/discordia-grupo01/servers/internal/repository/role_repo"
)

func newRepo(t *testing.T) (*role_repo.RoleRepository, *repository.ServerRepository, *pgxpool.Pool) {
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
		"../../../migrations/0004_default_role.sql",
	} {
		schema, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read migration %s: %v", file, err)
		}
		if _, err := pool.Exec(ctx, string(schema)); err != nil {
			t.Fatalf("apply migration %s: %v", file, err)
		}
	}
	return role_repo.NewRoleRepository(pool), repository.NewServerRepository(pool), pool
}

func serverFixture(owner string) (model.Server, []model.Channel) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	id := uuid.New()
	return model.Server{ID: id, Name: "Test Server", NameKey: "test server", OwnerID: owner, CreatedAt: now}, nil
}

func roleFixture(serverID uuid.UUID, name string) role.Role {
	now := time.Now().UTC().Truncate(time.Millisecond)
	return role.Role{ID: uuid.New(), ServerID: serverID, Name: name, Color: "#FF00AA", Permissions: 0, CreatedAt: now, UpdatedAt: now}
}

func TestRoleRepo_Create_ThenGetByID_ReturnsIt(t *testing.T) {
	roles, servers, _ := newRepo(t)
	ctx := context.Background()
	srv, chans := serverFixture("owner-" + uuid.NewString())
	if err := servers.CreateServer(ctx, srv, chans); err != nil {
		t.Fatalf("seed server: %v", err)
	}

	rl := roleFixture(srv.ID, "Moderator")
	if err := roles.Create(ctx, rl); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := roles.GetByID(ctx, rl.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Name != "Moderator" || got.ServerID != srv.ID {
		t.Errorf("got = %+v", got)
	}
}

func TestRoleRepo_GetByID_NotFound_ReturnsErrRoleNotFound(t *testing.T) {
	roles, _, _ := newRepo(t)
	_, err := roles.GetByID(context.Background(), uuid.New())
	if !errors.Is(err, model.ErrRoleNotFound) {
		t.Errorf("err = %v, want ErrRoleNotFound", err)
	}
}

func TestRoleRepo_ListByServer_ReturnsOnlyThatServersRoles(t *testing.T) {
	roles, servers, _ := newRepo(t)
	ctx := context.Background()
	srvA, chA := serverFixture("owner-" + uuid.NewString())
	srvB, chB := serverFixture("owner-" + uuid.NewString())
	if err := servers.CreateServer(ctx, srvA, chA); err != nil {
		t.Fatalf("seed server A: %v", err)
	}
	if err := servers.CreateServer(ctx, srvB, chB); err != nil {
		t.Fatalf("seed server B: %v", err)
	}

	if err := roles.Create(ctx, roleFixture(srvA.ID, "A1")); err != nil {
		t.Fatal(err)
	}
	if err := roles.Create(ctx, roleFixture(srvA.ID, "A2")); err != nil {
		t.Fatal(err)
	}
	if err := roles.Create(ctx, roleFixture(srvB.ID, "B1")); err != nil {
		t.Fatal(err)
	}

	got, err := roles.ListByServer(ctx, srvA.ID)
	if err != nil {
		t.Fatalf("ListByServer: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d roles, want 2", len(got))
	}
}

func TestRoleRepo_Create_RoleOfDeletedServer_Fails(t *testing.T) {
	roles, _, _ := newRepo(t)
	err := roles.Create(context.Background(), roleFixture(uuid.New(), "Orphan"))
	if err == nil {
		t.Error("expected a foreign key violation for a nonexistent server")
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
