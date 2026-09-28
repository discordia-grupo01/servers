package role_repo_test

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model/role"
	"github.com/discordia-grupo01/servers/internal/repository/role_repo"
)

func seedServer(ctx context.Context, t *testing.T) (*role_repo.RoleRepository, uuid.UUID) {
	t.Helper()
	roles, servers, _ := newRepo(t)
	srv, chans := serverFixture("owner-" + uuid.NewString())
	if err := servers.CreateServer(ctx, srv, nil, chans, testOutboxEvent()); err != nil {
		t.Fatalf("seed server: %v", err)
	}
	return roles, srv.ID
}

func TestRoleRepo_Create_AppendsAtTheBottomOfTheHierarchy(t *testing.T) {
	ctx := context.Background()
	roles, serverID := seedServer(ctx, t)

	for i, name := range []string{"Admin", "Mods", "Members"} {
		rl, err := roles.Create(ctx, roleFixture(serverID, name))
		if err != nil {
			t.Fatalf("Create %s: %v", name, err)
		}
		if rl.Position != i+1 {
			t.Errorf("%s: returned position = %d, want %d", name, rl.Position, i+1)
		}
		got, err := roles.GetByID(ctx, rl.ID)
		if err != nil {
			t.Fatalf("GetByID: %v", err)
		}
		if got.Position != i+1 {
			t.Errorf("%s: stored position = %d, want %d", name, got.Position, i+1)
		}
	}
}

func TestRoleRepo_Create_PositionsAreIndependentPerServer(t *testing.T) {
	ctx := context.Background()
	roles, servers, _ := newRepo(t)
	srvA, chA := serverFixture("owner-" + uuid.NewString())
	srvB, chB := serverFixture("owner-" + uuid.NewString())
	if err := servers.CreateServer(ctx, srvA, nil, chA, testOutboxEvent()); err != nil {
		t.Fatal(err)
	}
	if err := servers.CreateServer(ctx, srvB, nil, chB, testOutboxEvent()); err != nil {
		t.Fatal(err)
	}
	if _, err := roles.Create(ctx, roleFixture(srvA.ID, "A1")); err != nil {
		t.Fatal(err)
	}
	b1, err := roles.Create(ctx, roleFixture(srvB.ID, "B1"))
	if err != nil {
		t.Fatal(err)
	}
	if b1.Position != 1 {
		t.Errorf("first role of server B got position %d, want 1", b1.Position)
	}
}

func TestRoleRepo_Create_AfterDeletingARole_StillGoesToTheBottom(t *testing.T) {
	ctx := context.Background()
	roles, serverID := seedServer(ctx, t)
	var created []role.Role
	for _, name := range []string{"R1", "R2", "R3"} {
		rl, err := roles.Create(ctx, roleFixture(serverID, name))
		if err != nil {
			t.Fatal(err)
		}
		created = append(created, rl)
	}
	if err := roles.Delete(ctx, created[1].ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	rl, err := roles.Create(ctx, roleFixture(serverID, "R4"))
	if err != nil {
		t.Fatal(err)
	}
	if rl.Position != 4 {
		t.Errorf("position = %d, want 4 (the gap left by R2 is not reused)", rl.Position)
	}
}

func TestRoleRepo_ListByServer_OrdersByPosition(t *testing.T) {
	ctx := context.Background()
	roles, servers, pool := newRepo(t)
	srv, chans := serverFixture("owner-" + uuid.NewString())
	if err := servers.CreateServer(ctx, srv, nil, chans, testOutboxEvent()); err != nil {
		t.Fatal(err)
	}
	a, _ := roles.Create(ctx, roleFixture(srv.ID, "A"))
	b, _ := roles.Create(ctx, roleFixture(srv.ID, "B"))
	c, _ := roles.Create(ctx, roleFixture(srv.ID, "C"))

	// Reverse the hierarchy directly (the reorder endpoint arrives later), so
	// the order can't come from created_at by accident.
	if _, err := pool.Exec(ctx, `
		UPDATE roles SET position = CASE id WHEN $1 THEN 3 WHEN $2 THEN 2 WHEN $3 THEN 1 END
		 WHERE id IN ($1, $2, $3)`, a.ID, b.ID, c.ID); err != nil {
		t.Fatalf("reverse positions: %v", err)
	}

	got, err := roles.ListByServer(ctx, srv.ID)
	if err != nil {
		t.Fatalf("ListByServer: %v", err)
	}
	var names string
	for _, rl := range got {
		names += fmt.Sprintf("%s%d ", rl.Name, rl.Position)
	}
	if names != "C1 B2 A3 " {
		t.Errorf("order = %q, want %q", names, "C1 B2 A3 ")
	}
}

// Concurrent creations in the same server must never collide on a position:
// the server row lock serializes them.
func TestRoleRepo_Create_ConcurrentCreationsGetDistinctPositions(t *testing.T) {
	ctx := context.Background()
	roles, serverID := seedServer(ctx, t)

	const n = 20
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, err := roles.Create(ctx, roleFixture(serverID, fmt.Sprintf("R%d", i))); err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent Create: %v", err)
	}

	got, err := roles.ListByServer(ctx, serverID)
	if err != nil {
		t.Fatalf("ListByServer: %v", err)
	}
	if len(got) != n {
		t.Fatalf("got %d roles, want %d", len(got), n)
	}
	for i, rl := range got {
		if rl.Position != i+1 {
			t.Errorf("roles[%d].Position = %d, want %d", i, rl.Position, i+1)
		}
	}
}

// The 0012 migration backfills existing roles 1..N per server by creation
// date, oldest on top, which is the order clients already showed.
func TestMigration0012_BackfillsPositionsByCreationDate(t *testing.T) {
	ctx := context.Background()
	_, _, pool := newRepo(t)

	// Rebuild the schema as it was before 0012.
	dropAllTestTables(ctx, pool)
	for _, file := range []string{
		"../../../migrations/0001_init.up.sql",
		"../../../migrations/0003_roles.up.sql",
	} {
		schema, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, string(schema)); err != nil {
			t.Fatalf("apply %s: %v", file, err)
		}
	}

	srvA, srvB := uuid.New(), uuid.New()
	for _, id := range []uuid.UUID{srvA, srvB} {
		if _, err := pool.Exec(ctx,
			`INSERT INTO servers (id, name, name_key, owner_id) VALUES ($1, 'Server', $2, 'owner')`,
			id, "s-"+id.String()); err != nil {
			t.Fatalf("seed server: %v", err)
		}
	}
	base := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	seed := []struct {
		server uuid.UUID
		name   string
		age    time.Duration
	}{
		{srvA, "newest", 3 * time.Hour},
		{srvA, "oldest", 0},
		{srvA, "middle", time.Hour},
		{srvB, "only", 0},
	}
	for _, s := range seed {
		if _, err := pool.Exec(ctx,
			`INSERT INTO roles (id, server_id, name, color, created_at) VALUES ($1, $2, $3, '#000000', $4)`,
			uuid.New(), s.server, s.name, base.Add(s.age)); err != nil {
			t.Fatalf("seed role: %v", err)
		}
	}

	migration, err := os.ReadFile("../../../migrations/0015_role_positions.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, string(migration)); err != nil {
		t.Fatalf("apply 0012: %v", err)
	}

	want := map[string]int{"oldest": 1, "middle": 2, "newest": 3, "only": 1}
	rows, err := pool.Query(ctx, `SELECT name, position FROM roles`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		var pos int
		if err := rows.Scan(&name, &pos); err != nil {
			t.Fatal(err)
		}
		if pos != want[name] {
			t.Errorf("%s: position = %d, want %d", name, pos, want[name])
		}
	}
}
