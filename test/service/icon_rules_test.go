package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/service"
)

func TestCreateServer_UnsupportedIcon_IsNotPersisted(t *testing.T) {
	_, err, repo, _ := createIconErr(t, iconParams("Con Icono"), model.ErrIconUnsupportedType)
	if !errors.Is(err, model.ErrIconUnsupportedType) {
		t.Fatalf("err = %v, want ErrIconUnsupportedType", err)
	}
	if repo.calls != 0 {
		t.Error("server was created with an invalid icon")
	}
}

func TestCreateServer_TooLargeIcon_IsNotPersisted(t *testing.T) {
	_, err, repo, _ := createIconErr(t, iconParams("Con Icono"), model.ErrIconTooLarge)
	if !errors.Is(err, model.ErrIconTooLarge) {
		t.Fatalf("err = %v, want ErrIconTooLarge", err)
	}
	if repo.calls != 0 {
		t.Error("server was created with a too-large icon")
	}
}

func TestCreateServer_WhenIconStoreFails_ServerIsNotCreated(t *testing.T) {
	_, err, repo, _ := createIconErr(t, iconParams("Con Icono"), errors.New("disk full"))
	if err == nil {
		t.Fatal("expected the icon store error to surface")
	}
	if repo.calls != 0 {
		t.Error("repository was called after the icon store failed")
	}
}

func TestCreateServer_EmptyIconIsTreatedAsNoIcon_GetsDefault(t *testing.T) {
	srv, err, _, icons := create(t, validParams("Sin Icono Real"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if icons.generateCalls != 1 {
		t.Error("a default icon should be generated when no icon is sent")
	}
	if srv.IconURL == nil {
		t.Error("icon_url should be set to the generated default icon")
	}
}

func TestCreateServer_WhenDefaultIconGenerationFails_ServerIsNotCreated(t *testing.T) {
	repo, icons := &fakeRepoHelper{}, &fakeIconsHelper{generateErr: errors.New("rng down")}
	_, err := service.New(repo, icons, repo, repo).CreateServer(context.Background(), validParams("Sin Icono"))
	if err == nil {
		t.Fatal("expected the icon generation error to surface")
	}
	if repo.calls != 0 {
		t.Error("server was created even though the default icon failed to generate")
	}
}

func TestCreateServer_WhenRepositoryFails_ReturnsTheError(t *testing.T) {
	_, err, _, _ := createRepoErr(t, validParams("Mi Comunidad"), errors.New("db down"))
	if err == nil {
		t.Fatal("expected the repository error to surface")
	}
}

func TestCreateServer_WhenRepositoryFailsAfterSavingIcon_TheIconIsRemoved(t *testing.T) {
	_, err, _, icons := createRepoErr(t, iconParams("Con Icono"), errors.New("db down"))
	if err == nil {
		t.Fatal("expected an error")
	}
	if !icons.deleteCalled {
		t.Error("the orphan icon was not cleaned up")
	}
}

func TestCreateServer_WhenNameIsTakenInRepository_ReturnsNameTaken(t *testing.T) {
	_, err, _, _ := createRepoErr(t, validParams("Repetido"), model.ErrNameTaken)
	if !errors.Is(err, model.ErrNameTaken) {
		t.Fatalf("err = %v, want ErrNameTaken", err)
	}
}
