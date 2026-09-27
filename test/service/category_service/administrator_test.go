package category_service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/model/role"
	"github.com/discordia-grupo01/servers/internal/service/category_service"
)

const testAdmin = "admin-1"

// Bug #22: a non-owner holding ADMINISTRATOR manages categories.
func TestAdministrator_CanManageCategories(t *testing.T) {
	srv := ownerServer()
	cat := model.Category{ID: uuid.New(), ServerID: srv.ID, Name: "old"}
	id1, id2 := uuid.New(), uuid.New()
	repo := &fakeCategories{getByIDCategory: cat, listResult: []model.Category{{ID: id1}, {ID: id2}}}
	svc := newServiceWithStandings(&fakeServers{server: srv}, repo, &fakeStandings{standing: adminStanding()})
	ctx := context.Background()

	if _, err := svc.CreateCategory(ctx, category_service.CreateCategoryParams{ServerID: srv.ID, Name: "Voice", UserID: testAdmin}); err != nil {
		t.Errorf("CreateCategory: %v", err)
	}
	if _, err := svc.UpdateCategory(ctx, category_service.UpdateCategoryParams{CategoryID: cat.ID, UserID: testAdmin, Name: "new"}); err != nil {
		t.Errorf("UpdateCategory: %v", err)
	}
	if err := svc.ReorderCategories(ctx, category_service.ReorderCategoriesParams{ServerID: srv.ID, UserID: testAdmin, CategoryIDs: []uuid.UUID{id2, id1}}); err != nil {
		t.Errorf("ReorderCategories: %v", err)
	}
}

func TestGranularManageChannels_CanManageCategories(t *testing.T) {
	srv := ownerServer()
	cat := model.Category{ID: uuid.New(), ServerID: srv.ID, Name: "old"}
	standing := role.Standing{IsMember: true, Permissions: role.PermManageChannels}
	svc := newServiceWithStandings(&fakeServers{server: srv}, &fakeCategories{getByIDCategory: cat}, &fakeStandings{standing: standing})

	if _, err := svc.CreateCategory(context.Background(), category_service.CreateCategoryParams{ServerID: srv.ID, Name: "Voice", UserID: "mod-1"}); err != nil {
		t.Errorf("CreateCategory: %v", err)
	}
}

func TestManageCategoriesWithoutPermission_Forbidden(t *testing.T) {
	srv := ownerServer()
	standing := role.Standing{IsMember: true, Permissions: role.PermSendMessages}
	svc := newServiceWithStandings(&fakeServers{server: srv}, &fakeCategories{}, &fakeStandings{standing: standing})

	_, err := svc.CreateCategory(context.Background(), category_service.CreateCategoryParams{ServerID: srv.ID, Name: "Voice", UserID: "member-1"})
	if !errors.Is(err, model.ErrForbiddenNotAdmin) {
		t.Errorf("err = %v, want ErrForbiddenNotAdmin", err)
	}
}

func TestCreateCategory_StandingLookupError_Propagates(t *testing.T) {
	boom := errors.New("db down")
	svc := newServiceWithStandings(&fakeServers{server: ownerServer()}, &fakeCategories{}, &fakeStandings{err: boom})

	_, err := svc.CreateCategory(context.Background(), category_service.CreateCategoryParams{
		ServerID: uuid.New(), Name: "Voice", UserID: testAdmin,
	})
	if !errors.Is(err, boom) {
		t.Errorf("err = %v, want %v", err, boom)
	}
}
