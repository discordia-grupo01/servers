package category_service_test

import (
	"context"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/service/category_service"
)

const testOwner = "owner-1"

type fakeServers struct {
	server model.Server
	getErr error
}

func (f *fakeServers) GetByID(_ context.Context, _ uuid.UUID) (model.Server, error) {
	if f.getErr != nil {
		return model.Server{}, f.getErr
	}
	return f.server, nil
}

type fakeCategories struct {
	createErr   error
	created     model.Category
	createCalls int

	getByIDCategory model.Category
	getByIDErr      error

	listResult []model.Category
	listErr    error

	updateErr  error
	updatedID  uuid.UUID
	updateName string

	deleteErr    error
	deletedID    uuid.UUID
	deleteCalled bool

	reorderErr   error
	reorderedIDs []uuid.UUID
}

func (f *fakeCategories) Create(_ context.Context, cat model.Category) error {
	f.createCalls++
	if f.createErr != nil {
		return f.createErr
	}
	f.created = cat
	return nil
}

func (f *fakeCategories) GetByID(_ context.Context, _ uuid.UUID) (model.Category, error) {
	if f.getByIDErr != nil {
		return model.Category{}, f.getByIDErr
	}
	return f.getByIDCategory, nil
}

func (f *fakeCategories) ListByServer(_ context.Context, _ uuid.UUID) ([]model.Category, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.listResult, nil
}

func (f *fakeCategories) Update(_ context.Context, id uuid.UUID, name string) error {
	f.updatedID = id
	f.updateName = name
	return f.updateErr
}

func (f *fakeCategories) Delete(_ context.Context, id uuid.UUID) error {
	f.deleteCalled = true
	f.deletedID = id
	return f.deleteErr
}

func (f *fakeCategories) ReorderPositions(_ context.Context, orderedIDs []uuid.UUID) error {
	f.reorderedIDs = orderedIDs
	return f.reorderErr
}

func ownerServer() model.Server {
	return model.Server{ID: uuid.New(), OwnerID: testOwner, Name: "Test Server"}
}

func newService(servers *fakeServers, repo *fakeCategories) *category_service.CategoryService {
	return category_service.New(servers, repo)
}
