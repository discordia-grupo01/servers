package role_repo_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model"
)

func TestRoleRepo_Delete_ThenGetByID_NotFound(t *testing.T) {
	roles, servers, _ := newRepo(t)
	ctx := context.Background()
	srv, chans := serverFixture("owner-" + uuid.NewString())
	if err := servers.CreateServer(ctx, srv, chans); err != nil {
		t.Fatalf("seed server: %v", err)
	}
	rl := roleFixture(srv.ID, "Moderator")
	if err := roles.Create(ctx, rl); err != nil {
		t.Fatalf("seed role: %v", err)
	}

	if err := roles.Delete(ctx, rl.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	_, err := roles.GetByID(ctx, rl.ID)
	if !errors.Is(err, model.ErrRoleNotFound) {
		t.Errorf("err = %v, want ErrRoleNotFound", err)
	}
}

func TestRoleRepo_Delete_NotFound_ReturnsErrRoleNotFound(t *testing.T) {
	roles, _, _ := newRepo(t)
	err := roles.Delete(context.Background(), uuid.New())
	if !errors.Is(err, model.ErrRoleNotFound) {
		t.Errorf("err = %v, want ErrRoleNotFound", err)
	}
}

func TestRoleRepo_Delete_IsServerDefault_Rejected(t *testing.T) {
	roles, servers, pool := newRepo(t)
	ctx := context.Background()
	srv, chans := serverFixture("owner-" + uuid.NewString())
	if err := servers.CreateServer(ctx, srv, chans); err != nil {
		t.Fatalf("seed server: %v", err)
	}
	rl := roleFixture(srv.ID, "Moderator")
	if err := roles.Create(ctx, rl); err != nil {
		t.Fatalf("seed role: %v", err)
	}
	if err := servers.SetDefaultRole(ctx, srv.ID, rl.ID); err != nil {
		t.Fatalf("SetDefaultRole: %v", err)
	}

	err := roles.Delete(ctx, rl.ID)
	if !errors.Is(err, model.ErrRoleIsServerDefault) {
		t.Fatalf("Delete err = %v, want ErrRoleIsServerDefault", err)
	}

	var defaultRoleID *uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT default_role_id FROM servers WHERE id = $1`, srv.ID).Scan(&defaultRoleID); err != nil {
		t.Fatalf("select default_role_id: %v", err)
	}
	if defaultRoleID == nil || *defaultRoleID != rl.ID {
		t.Errorf("default_role_id = %v, want %v (delete should be a no-op)", defaultRoleID, rl.ID)
	}
	if _, err := roles.GetByID(ctx, rl.ID); err != nil {
		t.Errorf("role should still exist after a rejected delete: %v", err)
	}
}

func TestDeleteRole_WithAssignments_CascadesMemberRoles(t *testing.T) {
	roles, servers, pool := newRepo(t)
	ctx := context.Background()
	srv, chans := serverFixture("owner-" + uuid.NewString())
	if err := servers.CreateServer(ctx, srv, chans); err != nil {
		t.Fatalf("seed server: %v", err)
	}
	rl := roleFixture(srv.ID, "Moderator")
	if err := roles.Create(ctx, rl); err != nil {
		t.Fatalf("seed role: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO member_roles (server_id, user_id, role_id) VALUES ($1, $2, $3)`,
		srv.ID, srv.OwnerID, rl.ID,
	); err != nil {
		t.Fatalf("seed member_role: %v", err)
	}

	if err := roles.Delete(ctx, rl.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM member_roles WHERE role_id = $1`, rl.ID).Scan(&count); err != nil {
		t.Fatalf("count member_roles: %v", err)
	}
	if count != 0 {
		t.Errorf("member_roles rows for deleted role = %d, want 0", count)
	}
}
