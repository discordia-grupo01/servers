package service_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/service"
)

const testOwner = "user-123"

type fakeRepoHelper struct {
	err       error
	calls     int
	gotServer model.Server
	gotCats   []model.Category
	gotChans  []model.Channel

	listResult  []model.Server
	listErr     error
	listCalls   int
	gotListUser string

	getResult model.Server
	getErr    error
	getCalls  int

	updateErr   error
	updateCalls int
}

func (f *fakeRepoHelper) CreateServer(_ context.Context, s model.Server, cats []model.Category, ch []model.Channel) error {
	f.calls++
	if f.err != nil {
		return f.err
	}
	f.gotServer, f.gotCats, f.gotChans = s, cats, ch
	return nil
}

func (f *fakeRepoHelper) ListForUser(_ context.Context, userID string) ([]model.Server, error) {
	f.listCalls++
	f.gotListUser = userID
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.listResult, nil
}

func (f *fakeRepoHelper) GetByID(_ context.Context, _ uuid.UUID) (model.Server, error) {
	f.getCalls++
	if f.getErr != nil {
		return model.Server{}, f.getErr
	}
	return f.getResult, nil
}

func (f *fakeRepoHelper) UpdateServer(_ context.Context, _ uuid.UUID, _, _ string, _, _ *string) error {
	f.updateCalls++
	return f.updateErr
}

type fakeIconsHelper struct {
	err          error
	savedID      string
	savedData    []byte
	deleteCalled bool

	generateCalls int
	generateData  []byte
	generateErr   error
}

func (f *fakeIconsHelper) Save(id string, data []byte) (string, error) {
	f.savedID, f.savedData = id, data
	if f.err != nil {
		return "", f.err
	}
	return "/v1/servers/" + id + "/icon", nil
}

func (f *fakeIconsHelper) Delete(string) error { f.deleteCalled = true; return nil }

func (f *fakeIconsHelper) GenerateDefault() ([]byte, error) {
	f.generateCalls++
	if f.generateErr != nil {
		return nil, f.generateErr
	}
	if f.generateData != nil {
		return f.generateData, nil
	}
	return []byte("generated-default-icon"), nil
}

func create(t *testing.T, p service.CreateServerParams) (model.Server, error, *fakeRepoHelper, *fakeIconsHelper) {
	t.Helper()
	repo, icons := &fakeRepoHelper{}, &fakeIconsHelper{}
	srv, err := service.New(repo, icons, &fakePublisher{}).CreateServer(context.Background(), p)
	return srv, err, repo, icons
}

func createRepoErr(t *testing.T, p service.CreateServerParams, repoErr error) (model.Server, error, *fakeRepoHelper, *fakeIconsHelper) {
	t.Helper()
	repo, icons := &fakeRepoHelper{err: repoErr}, &fakeIconsHelper{}
	srv, err := service.New(repo, icons, &fakePublisher{}).CreateServer(context.Background(), p)
	return srv, err, repo, icons
}

func createIconErr(t *testing.T, p service.CreateServerParams, iconErr error) (model.Server, error, *fakeRepoHelper, *fakeIconsHelper) {
	t.Helper()
	repo, icons := &fakeRepoHelper{}, &fakeIconsHelper{err: iconErr}
	srv, err := service.New(repo, icons, &fakePublisher{}).CreateServer(context.Background(), p)
	return srv, err, repo, icons
}

func createWithPublisher(t *testing.T, p service.CreateServerParams, pub *fakePublisher) (model.Server, error, *fakeRepoHelper, *fakeIconsHelper) {
	t.Helper()
	repo, icons := &fakeRepoHelper{}, &fakeIconsHelper{}
	srv, err := service.New(repo, icons, pub).CreateServer(context.Background(), p)
	return srv, err, repo, icons
}

func createRepoErrWithPublisher(t *testing.T, p service.CreateServerParams, repoErr error, pub *fakePublisher) (model.Server, error, *fakeRepoHelper, *fakeIconsHelper) {
	t.Helper()
	repo, icons := &fakeRepoHelper{err: repoErr}, &fakeIconsHelper{}
	srv, err := service.New(repo, icons, pub).CreateServer(context.Background(), p)
	return srv, err, repo, icons
}

func validParams(name string) service.CreateServerParams {
	return service.CreateServerParams{Name: name, OwnerID: testOwner}
}

func iconParams(name string) service.CreateServerParams {
	return service.CreateServerParams{Name: name, OwnerID: testOwner, Icon: []byte("some-bytes")}
}
