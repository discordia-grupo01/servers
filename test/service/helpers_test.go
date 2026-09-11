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
	gotChans  []model.Channel

	listResult  []model.Server
	listErr     error
	listCalls   int
	gotListUser string

	getResult model.Server
	getErr    error
	getCalls  int
}

func (f *fakeRepoHelper) CreateServer(_ context.Context, s model.Server, ch []model.Channel) error {
	f.calls++
	if f.err != nil {
		return f.err
	}
	f.gotServer, f.gotChans = s, ch
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
	srv, err := service.New(repo, icons).CreateServer(context.Background(), p)
	return srv, err, repo, icons
}

func createRepoErr(t *testing.T, p service.CreateServerParams, repoErr error) (model.Server, error, *fakeRepoHelper, *fakeIconsHelper) {
	t.Helper()
	repo, icons := &fakeRepoHelper{err: repoErr}, &fakeIconsHelper{}
	srv, err := service.New(repo, icons).CreateServer(context.Background(), p)
	return srv, err, repo, icons
}

func createIconErr(t *testing.T, p service.CreateServerParams, iconErr error) (model.Server, error, *fakeRepoHelper, *fakeIconsHelper) {
	t.Helper()
	repo, icons := &fakeRepoHelper{}, &fakeIconsHelper{err: iconErr}
	srv, err := service.New(repo, icons).CreateServer(context.Background(), p)
	return srv, err, repo, icons
}

func validParams(name string) service.CreateServerParams {
	return service.CreateServerParams{Name: name, OwnerID: testOwner}
}

func iconParams(name string) service.CreateServerParams {
	return service.CreateServerParams{Name: name, OwnerID: testOwner, Icon: []byte("some-bytes")}
}
