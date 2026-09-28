package category_service_test

import (
	"context"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/model/role"
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

	updateErr     error
	updatedID     uuid.UUID
	updateName    string
	updateNameKey string

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

func (f *fakeCategories) Update(_ context.Context, id uuid.UUID, name, nameKey string) error {
	f.updatedID = id
	f.updateName = name
	f.updateNameKey = nameKey
	return f.updateErr
}

func (f *fakeCategories) ReorderPositions(_ context.Context, orderedIDs []uuid.UUID) error {
	f.reorderedIDs = orderedIDs
	return f.reorderErr
}

func ownerServer() model.Server {
	return model.Server{ID: uuid.New(), OwnerID: testOwner, Name: "Test Server"}
}

func newService(servers *fakeServers, repo *fakeCategories) *category_service.CategoryService {
	return newServiceWithStandings(servers, repo, &fakeStandings{})
}

func newServiceWithStandings(servers *fakeServers, repo *fakeCategories, standings *fakeStandings) *category_service.CategoryService {
	return category_service.New(servers, repo, standings)
}

// fakeStandings returns the same standing for every non-owner caller (the
// owner never reaches it: service.ResolveStanding short-circuits them). The
// zero value makes every non-owner a non-member, i.e. not an administrator.
type fakeStandings struct {
	standing role.Standing
	err      error
}

func (f *fakeStandings) MemberStanding(_ context.Context, _ uuid.UUID, _ string) (role.Standing, error) {
	if f.err != nil {
		return role.Standing{}, f.err
	}
	return f.standing, nil
}

// adminStanding is a non-owner member holding ADMINISTRATOR through a role.
func adminStanding() role.Standing {
	return role.Standing{IsMember: true, Permissions: role.PermAdministrator}
}
