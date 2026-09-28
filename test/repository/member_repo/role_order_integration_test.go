package member_repo_test

import (
	"context"
	"testing"
)

// A member's roles come back highest first (by position), not by creation
// date.
func TestMemberRepo_ListRolesForMember_OrdersByPosition(t *testing.T) {
	members, servers, roles, pool := newRepo(t)
	ctx := context.Background()
	srv, member := seedServerWithMember(ctx, t, servers, pool)
	older := seedRole(ctx, t, roles, srv.ID, "Older")
	newer := seedRole(ctx, t, roles, srv.ID, "Newer")
	for _, id := range []any{older.ID, newer.ID} {
		if _, err := pool.Exec(ctx,
			`INSERT INTO member_roles (server_id, user_id, role_id) VALUES ($1, $2, $3)`,
			srv.ID, member, id); err != nil {
			t.Fatalf("assign role: %v", err)
		}
	}
	// Put the newer role on top.
	if _, err := pool.Exec(ctx,
		`UPDATE roles SET position = CASE id WHEN $1 THEN 2 ELSE 1 END WHERE id IN ($1, $2)`,
		older.ID, newer.ID); err != nil {
		t.Fatalf("swap positions: %v", err)
	}

	got, err := members.ListRolesForMember(ctx, srv.ID, member)
	if err != nil {
		t.Fatalf("ListRolesForMember: %v", err)
	}
	if len(got) != 2 || got[0].ID != newer.ID || got[0].Position != 1 || got[1].ID != older.ID || got[1].Position != 2 {
		t.Errorf("got = %+v, want [Newer(1), Older(2)]", got)
	}
}
