package service_test

import (
	"errors"
	"testing"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/service"
)

func TestRequireManageRoles_Owner_Allowed(t *testing.T) {
	srv := model.Server{OwnerID: "owner-1"}
	if err := service.RequireManageRoles(srv, "owner-1"); err != nil {
		t.Errorf("unexpected error for the owner: %v", err)
	}
}

func TestRequireManageRoles_NotOwner_Forbidden(t *testing.T) {
	srv := model.Server{OwnerID: "owner-1"}
	err := service.RequireManageRoles(srv, "someone-else")
	if !errors.Is(err, model.ErrForbiddenNotOwner) {
		t.Errorf("err = %v, want ErrForbiddenNotOwner", err)
	}
}
