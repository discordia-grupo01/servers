package role_service_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model/role"
)

// The position is assigned by the repository (bottom of the hierarchy); the
// service must hand back the role as persisted, not the one it built.
func TestCreateRole_ReturnsThePositionAssignedByTheRepository(t *testing.T) {
	srv := ownerServer()
	roles := &fakeRoles{listRoles: []role.Role{
		{ID: uuid.New(), ServerID: srv.ID, Name: "Admin", Position: 1},
		{ID: uuid.New(), ServerID: srv.ID, Name: "Mods", Position: 2},
	}}
	svc := newService(&fakeServers{server: srv}, roles)

	rl, err := svc.CreateRole(context.Background(), validCreateParams(srv.ID))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rl.Position != 3 {
		t.Errorf("position = %d, want 3 (bottom of the hierarchy)", rl.Position)
	}
}
