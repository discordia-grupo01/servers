package category_service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/service/category_service"
)

func TestCreateCategory_Valid_CreatesIt(t *testing.T) {
	srv := ownerServer()
	repo := &fakeCategories{}
	svc := newService(&fakeServers{server: srv}, repo)

	cat, err := svc.CreateCategory(context.Background(), category_service.CreateCategoryParams{
		ServerID: srv.ID, Name: "Texto", UserID: testOwner,
	})
	if err != nil {
		t.Fatalf("CreateCategory: %v", err)
	}
	if cat.Name != "Texto" || cat.ServerID != srv.ID {
		t.Errorf("category = %+v", cat)
	}
	if repo.createCalls != 1 {
		t.Errorf("createCalls = %d, want 1", repo.createCalls)
	}
}

func TestCreateCategory_EmptyName_ErrNameRequired(t *testing.T) {
	svc := newService(&fakeServers{server: ownerServer()}, &fakeCategories{})
	_, err := svc.CreateCategory(context.Background(), category_service.CreateCategoryParams{
		ServerID: uuid.New(), Name: "   ", UserID: testOwner,
	})
	if !errors.Is(err, model.ErrCategoryNameRequired) {
		t.Errorf("err = %v, want ErrCategoryNameRequired", err)
	}
}

func TestCreateCategory_ControlCharInName_ErrNameInvalidChars(t *testing.T) {
	svc := newService(&fakeServers{server: ownerServer()}, &fakeCategories{})
	_, err := svc.CreateCategory(context.Background(), category_service.CreateCategoryParams{
		ServerID: uuid.New(), Name: "bad\x00name", UserID: testOwner,
	})
	if !errors.Is(err, model.ErrCategoryNameInvalidChars) {
		t.Errorf("err = %v, want ErrCategoryNameInvalidChars", err)
	}
}

func TestCreateCategory_NotOwner_ErrForbidden(t *testing.T) {
	svc := newService(&fakeServers{server: ownerServer()}, &fakeCategories{})
	_, err := svc.CreateCategory(context.Background(), category_service.CreateCategoryParams{
		ServerID: uuid.New(), Name: "Texto", UserID: "someone-else",
	})
	if !errors.Is(err, model.ErrForbiddenNotOwner) {
		t.Errorf("err = %v, want ErrForbiddenNotOwner", err)
	}
}

func TestReorderCategories_ExactCurrentSet_Reorders(t *testing.T) {
	srv := ownerServer()
	id1, id2 := uuid.New(), uuid.New()
	repo := &fakeCategories{listResult: []model.Category{{ID: id1}, {ID: id2}}}
	svc := newService(&fakeServers{server: srv}, repo)

	err := svc.ReorderCategories(context.Background(), category_service.ReorderCategoriesParams{
		ServerID: srv.ID, UserID: testOwner, CategoryIDs: []uuid.UUID{id2, id1},
	})
	if err != nil {
		t.Fatalf("ReorderCategories: %v", err)
	}
	if len(repo.reorderedIDs) != 2 || repo.reorderedIDs[0] != id2 || repo.reorderedIDs[1] != id1 {
		t.Errorf("reorderedIDs = %v", repo.reorderedIDs)
	}
}

func TestReorderCategories_MissingCategory_ErrReorderInvalid(t *testing.T) {
	srv := ownerServer()
	id1, id2 := uuid.New(), uuid.New()
	repo := &fakeCategories{listResult: []model.Category{{ID: id1}, {ID: id2}}}
	svc := newService(&fakeServers{server: srv}, repo)

	err := svc.ReorderCategories(context.Background(), category_service.ReorderCategoriesParams{
		ServerID: srv.ID, UserID: testOwner, CategoryIDs: []uuid.UUID{id1},
	})
	if !errors.Is(err, model.ErrCategoryReorderInvalid) {
		t.Errorf("err = %v, want ErrCategoryReorderInvalid", err)
	}
	if repo.reorderedIDs != nil {
		t.Errorf("repo.ReorderPositions should not have been called")
	}
}

func TestReorderCategories_NotOwner_ErrForbidden(t *testing.T) {
	srv := ownerServer()
	svc := newService(&fakeServers{server: srv}, &fakeCategories{})

	err := svc.ReorderCategories(context.Background(), category_service.ReorderCategoriesParams{
		ServerID: srv.ID, UserID: "someone-else", CategoryIDs: []uuid.UUID{},
	})
	if !errors.Is(err, model.ErrForbiddenNotOwner) {
		t.Errorf("err = %v, want ErrForbiddenNotOwner", err)
	}
}
