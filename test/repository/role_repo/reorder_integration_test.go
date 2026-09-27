package role_repo_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model/role"
	"github.com/discordia-grupo01/servers/internal/repository/role_repo"
)

func seedRoles(ctx context.Context, t *testing.T, names ...string) (*role_repo.RoleRepository, uuid.UUID, []uuid.UUID) {
	t.Helper()
	roles, srvID := seedServer(ctx, t)
	var ids []uuid.UUID
	for _, name := range names {
		rl, err := roles.Create(ctx, roleFixture(srvID, name))
		if err != nil {
			t.Fatalf("seed role %s: %v", name, err)
		}
		ids = append(ids, rl.ID)
	}
	return roles, srvID, ids
}

func roleNames(roles []role.Role) []string {
	out := make([]string, 0, len(roles))
	for _, rl := range roles {
		out = append(out, rl.Name)
	}
	return out
}

func TestRoleRepo_Reorder_RewritesPositionsOneToN(t *testing.T) {
	ctx := context.Background()
	roles, serverID, ids := seedRoles(ctx, t, "A", "B", "C")
	a, b, c := ids[0], ids[1], ids[2]

	var seen []role.Role
	got, err := roles.Reorder(ctx, serverID, []uuid.UUID{c, a, b}, func(current []role.Role) error {
		seen = current
		return nil
	})
	if err != nil {
		t.Fatalf("Reorder: %v", err)
	}
	if n := roleNames(seen); len(n) != 3 || n[0] != "A" || n[2] != "C" {
		t.Errorf("check saw %v, want the current order A B C", n)
	}
	for i, want := range []string{"C", "A", "B"} {
		if got[i].Name != want || got[i].Position != i+1 {
			t.Errorf("returned[%d] = %s(%d), want %s(%d)", i, got[i].Name, got[i].Position, want, i+1)
		}
	}
	listed, err := roles.ListByServer(ctx, serverID)
	if err != nil {
		t.Fatal(err)
	}
	if n := roleNames(listed); n[0] != "C" || n[1] != "A" || n[2] != "B" {
		t.Errorf("persisted order = %v, want C A B", n)
	}
}

func TestRoleRepo_Reorder_CheckErrorWritesNothing(t *testing.T) {
	ctx := context.Background()
	roles, serverID, ids := seedRoles(ctx, t, "A", "B")
	rejected := errors.New("rejected")

	_, err := roles.Reorder(ctx, serverID, []uuid.UUID{ids[1], ids[0]}, func([]role.Role) error { return rejected })
	if !errors.Is(err, rejected) {
		t.Fatalf("err = %v, want the check's error as-is", err)
	}
	listed, _ := roles.ListByServer(ctx, serverID)
	if n := roleNames(listed); n[0] != "A" || n[1] != "B" {
		t.Errorf("order changed to %v after a rejected reorder", n)
	}
}

func TestRoleRepo_Reorder_FillsGapsLeftByDeletions(t *testing.T) {
	ctx := context.Background()
	roles, servers, _ := newRepo(t)
	srv, chans := serverFixture("owner-" + uuid.NewString())
	if err := servers.CreateServer(ctx, srv, nil, chans, testOutboxEvent()); err != nil {
		t.Fatal(err)
	}
	a, _ := roles.Create(ctx, roleFixture(srv.ID, "A"))
	b, _ := roles.Create(ctx, roleFixture(srv.ID, "B"))
	c, _ := roles.Create(ctx, roleFixture(srv.ID, "C"))
	if err := roles.Delete(ctx, b.ID); err != nil {
		t.Fatal(err)
	}

	got, err := roles.Reorder(ctx, srv.ID, []uuid.UUID{c.ID, a.ID}, func([]role.Role) error { return nil })
	if err != nil {
		t.Fatalf("Reorder: %v", err)
	}
	if got[0].Position != 1 || got[1].Position != 2 {
		t.Errorf("positions = %d, %d; want 1, 2", got[0].Position, got[1].Position)
	}
}

// A reorder and a creation racing on the same server are serialized by the
// server-row lock: the check always sees a consistent list, and positions
// never collide.
func TestRoleRepo_Reorder_ConcurrentWithCreate_NoDuplicatePositions(t *testing.T) {
	ctx := context.Background()
	roles, serverID, ids := seedRoles(ctx, t, "A", "B", "C")

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, _ = roles.Create(ctx, roleFixture(serverID, "new"))
		}()
		go func() {
			defer wg.Done()
			// The list may be stale if a creation committed first; then the
			// check rejects it -- exactly what the service does.
			_, _ = roles.Reorder(ctx, serverID, []uuid.UUID{ids[2], ids[1], ids[0]}, func(current []role.Role) error {
				if len(current) != 3 {
					return errors.New("stale list")
				}
				return nil
			})
		}()
	}
	wg.Wait()

	listed, err := roles.ListByServer(ctx, serverID)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[int]bool{}
	for _, rl := range listed {
		if seen[rl.Position] {
			t.Fatalf("duplicate position %d in %v", rl.Position, roleNames(listed))
		}
		seen[rl.Position] = true
	}
	if len(listed) != 13 {
		t.Errorf("got %d roles, want 13", len(listed))
	}
}
