package category_service_test

import (
	"context"
	"errors"
	"strings"
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

func TestCreateCategory_NotAdmin_ErrForbidden(t *testing.T) {
	svc := newService(&fakeServers{server: ownerServer()}, &fakeCategories{})
	_, err := svc.CreateCategory(context.Background(), category_service.CreateCategoryParams{
		ServerID: uuid.New(), Name: "Texto", UserID: "someone-else",
	})
	if !errors.Is(err, model.ErrForbiddenNotAdmin) {
		t.Errorf("err = %v, want ErrForbiddenNotAdmin", err)
	}
}

func TestCreateCategory_NameTakenFromRepo_Propagates(t *testing.T) {
	svc := newService(&fakeServers{server: ownerServer()}, &fakeCategories{createErr: model.ErrCategoryNameTaken})
	_, err := svc.CreateCategory(context.Background(), category_service.CreateCategoryParams{
		ServerID: uuid.New(), Name: "Eventos", UserID: testOwner,
	})
	if !errors.Is(err, model.ErrCategoryNameTaken) {
		t.Errorf("err = %v, want ErrCategoryNameTaken", err)
	}
}

func TestCreateCategory_ComputesNameKey_CaseAndAccentInsensitive(t *testing.T) {
	srv := ownerServer()
	repo := &fakeCategories{}
	svc := newService(&fakeServers{server: srv}, repo)

	_, err := svc.CreateCategory(context.Background(), category_service.CreateCategoryParams{
		ServerID: srv.ID, Name: "  Eventós  Especiales ", UserID: testOwner,
	})
	if err != nil {
		t.Fatalf("CreateCategory: %v", err)
	}
	if repo.created.NameKey != "eventos especiales" {
		t.Errorf("created.NameKey = %q, want %q", repo.created.NameKey, "eventos especiales")
	}
}

func TestUpdateCategory_Valid_RenamesIt(t *testing.T) {
	srv := ownerServer()
	cat := model.Category{ID: uuid.New(), ServerID: srv.ID, Name: "old"}
	repo := &fakeCategories{getByIDCategory: cat}
	svc := newService(&fakeServers{server: srv}, repo)

	got, err := svc.UpdateCategory(context.Background(), category_service.UpdateCategoryParams{
		CategoryID: cat.ID, UserID: testOwner, Name: "new-name",
	})
	if err != nil {
		t.Fatalf("UpdateCategory: %v", err)
	}
	if got.Name != "new-name" {
		t.Errorf("got.Name = %q, want new-name", got.Name)
	}
	if repo.updatedID != cat.ID || repo.updateName != "new-name" {
		t.Errorf("repo.Update called with id=%v name=%q", repo.updatedID, repo.updateName)
	}
	if repo.updateNameKey != "new-name" {
		t.Errorf("repo.updateNameKey = %q, want %q", repo.updateNameKey, "new-name")
	}
}

func TestUpdateCategory_EmptyName_ErrNameRequired(t *testing.T) {
	srv := ownerServer()
	cat := model.Category{ID: uuid.New(), ServerID: srv.ID}
	svc := newService(&fakeServers{server: srv}, &fakeCategories{getByIDCategory: cat})

	_, err := svc.UpdateCategory(context.Background(), category_service.UpdateCategoryParams{
		CategoryID: cat.ID, UserID: testOwner, Name: "   ",
	})
	if !errors.Is(err, model.ErrCategoryNameRequired) {
		t.Errorf("err = %v, want ErrCategoryNameRequired", err)
	}
}

func TestUpdateCategory_NameTakenFromRepo_Propagates(t *testing.T) {
	srv := ownerServer()
	cat := model.Category{ID: uuid.New(), ServerID: srv.ID, Name: "old"}
	repo := &fakeCategories{getByIDCategory: cat, updateErr: model.ErrCategoryNameTaken}
	svc := newService(&fakeServers{server: srv}, repo)

	_, err := svc.UpdateCategory(context.Background(), category_service.UpdateCategoryParams{
		CategoryID: cat.ID, UserID: testOwner, Name: "Eventos",
	})
	if !errors.Is(err, model.ErrCategoryNameTaken) {
		t.Errorf("err = %v, want ErrCategoryNameTaken", err)
	}
}

func TestUpdateCategory_NotAdmin_ErrForbidden(t *testing.T) {
	srv := ownerServer()
	cat := model.Category{ID: uuid.New(), ServerID: srv.ID}
	svc := newService(&fakeServers{server: srv}, &fakeCategories{getByIDCategory: cat})

	_, err := svc.UpdateCategory(context.Background(), category_service.UpdateCategoryParams{
		CategoryID: cat.ID, UserID: "someone-else", Name: "new-name",
	})
	if !errors.Is(err, model.ErrForbiddenNotAdmin) {
		t.Errorf("err = %v, want ErrForbiddenNotAdmin", err)
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

func TestReorderCategories_NotAdmin_ErrForbidden(t *testing.T) {
	srv := ownerServer()
	svc := newService(&fakeServers{server: srv}, &fakeCategories{})

	err := svc.ReorderCategories(context.Background(), category_service.ReorderCategoriesParams{
		ServerID: srv.ID, UserID: "someone-else", CategoryIDs: []uuid.UUID{},
	})
	if !errors.Is(err, model.ErrForbiddenNotAdmin) {
		t.Errorf("err = %v, want ErrForbiddenNotAdmin", err)
	}
}

func TestCreateCategory_NameLength_MaxIsAllowed(t *testing.T) {
	cases := map[string]struct {
		length  int
		wantErr error
	}{
		"exactly the max": {length: model.CategoryNameMaxLen},
		"one over":        {length: model.CategoryNameMaxLen + 1, wantErr: model.ErrCategoryNameTooLong},
	}
	for label, tc := range cases {
		t.Run(label, func(t *testing.T) {
			srv := ownerServer()
			svc := newService(&fakeServers{server: srv}, &fakeCategories{})

			_, err := svc.CreateCategory(context.Background(), category_service.CreateCategoryParams{
				ServerID: srv.ID, Name: strings.Repeat("á", tc.length), UserID: testOwner,
			})
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestReorderCategories_RepoError_Propagates(t *testing.T) {
	srv := ownerServer()
	id1, id2 := uuid.New(), uuid.New()
	repoErr := errors.New("db down")
	repo := &fakeCategories{listResult: []model.Category{{ID: id1}, {ID: id2}}, reorderErr: repoErr}
	svc := newService(&fakeServers{server: srv}, repo)

	err := svc.ReorderCategories(context.Background(), category_service.ReorderCategoriesParams{
		ServerID: srv.ID, UserID: testOwner, CategoryIDs: []uuid.UUID{id2, id1},
	})
	if !errors.Is(err, repoErr) {
		t.Errorf("err = %v, want the repository error", err)
	}
}
