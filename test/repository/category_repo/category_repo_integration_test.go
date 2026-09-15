package category_repo_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/repository"
	"github.com/discordia-grupo01/servers/internal/repository/category_repo"
)

func newRepo(t *testing.T) (*category_repo.CategoryRepository, *repository.ServerRepository, *pgxpool.Pool) {
	t.Helper()
	host := os.Getenv("DATABASE_HOST")
	if host == "" {
		t.Skip("DATABASE_HOST not set: skipping integration test")
	}
	dsn := "host=" + host +
		" port=" + env("DATABASE_PORT", "5432") +
		" dbname=" + env("DATABASE_NAME", "discordia_servicio") +
		" user=" + env("DATABASE_USER", "discordia") +
		" password=" + env("DATABASE_PASSWORD", "discordia") +
		" sslmode=disable"

	ctx := context.Background()
	pool, err := repository.NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	t.Cleanup(pool.Close)
	t.Cleanup(func() { dropAllTestTables(ctx, pool) })

	dropAllTestTables(ctx, pool)
	for _, file := range []string{
		"../../../migrations/0001_init.up.sql",
		"../../../migrations/0007_categories.up.sql",
		"../../../migrations/0010_native_categories.up.sql",
	} {
		schema, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read migration %s: %v", file, err)
		}
		if _, err := pool.Exec(ctx, string(schema)); err != nil {
			t.Fatalf("apply migration %s: %v", file, err)
		}
	}
	return category_repo.NewCategoryRepository(pool), repository.NewServerRepository(pool), pool
}

func serverFixture(owner string) (model.Server, []model.Channel) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	id := uuid.New()
	return model.Server{ID: id, Name: "Test Server", NameKey: "test server", OwnerID: owner, CreatedAt: now}, nil
}

func categoryFixture(serverID uuid.UUID, name string, position int) model.Category {
	return model.Category{
		ID: uuid.New(), ServerID: serverID, Name: name, Position: position,
		CreatedAt: time.Now().UTC().Truncate(time.Millisecond),
	}
}

func TestCategoryRepo_Create_ThenGetByID_ReturnsIt(t *testing.T) {
	categories, servers, _ := newRepo(t)
	ctx := context.Background()
	srv, chans := serverFixture("owner-" + uuid.NewString())
	if err := servers.CreateServer(ctx, srv, nil, chans); err != nil {
		t.Fatalf("seed server: %v", err)
	}

	cat := categoryFixture(srv.ID, "Texto", 0)
	if err := categories.Create(ctx, cat); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := categories.GetByID(ctx, cat.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Name != "Texto" || got.ServerID != srv.ID {
		t.Errorf("got = %+v", got)
	}
}

func TestCategoryRepo_Create_WithDefaultKind_RoundTripsThroughGetByID(t *testing.T) {
	categories, servers, _ := newRepo(t)
	ctx := context.Background()
	srv, chans := serverFixture("owner-" + uuid.NewString())
	if err := servers.CreateServer(ctx, srv, nil, chans); err != nil {
		t.Fatalf("seed server: %v", err)
	}

	cat := categoryFixture(srv.ID, "Canales de texto", 0)
	textKind := model.ChannelText
	cat.DefaultKind = &textKind
	if err := categories.Create(ctx, cat); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := categories.GetByID(ctx, cat.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.DefaultKind == nil || *got.DefaultKind != model.ChannelText {
		t.Errorf("DefaultKind = %v, want %q", got.DefaultKind, model.ChannelText)
	}
}

func TestCategoryRepo_Create_WithoutDefaultKind_StaysNil(t *testing.T) {
	categories, servers, _ := newRepo(t)
	ctx := context.Background()
	srv, chans := serverFixture("owner-" + uuid.NewString())
	if err := servers.CreateServer(ctx, srv, nil, chans); err != nil {
		t.Fatalf("seed server: %v", err)
	}

	cat := categoryFixture(srv.ID, "Owner-made category", 0)
	if err := categories.Create(ctx, cat); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := categories.GetByID(ctx, cat.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.DefaultKind != nil {
		t.Errorf("DefaultKind = %v, want nil", *got.DefaultKind)
	}
}

func TestCategoryRepo_GetByID_NotFound_ReturnsErrCategoryNotFound(t *testing.T) {
	categories, _, _ := newRepo(t)
	_, err := categories.GetByID(context.Background(), uuid.New())
	if !errors.Is(err, model.ErrCategoryNotFound) {
		t.Errorf("err = %v, want ErrCategoryNotFound", err)
	}
}

func TestCategoryRepo_ListByServer_ReturnsOnlyThatServersCategories_OrderedByPosition(t *testing.T) {
	categories, servers, _ := newRepo(t)
	ctx := context.Background()
	srvA, chA := serverFixture("owner-" + uuid.NewString())
	srvB, chB := serverFixture("owner-" + uuid.NewString())
	if err := servers.CreateServer(ctx, srvA, nil, chA); err != nil {
		t.Fatalf("seed server A: %v", err)
	}
	if err := servers.CreateServer(ctx, srvB, nil, chB); err != nil {
		t.Fatalf("seed server B: %v", err)
	}

	second := categoryFixture(srvA.ID, "Second", 1)
	first := categoryFixture(srvA.ID, "First", 0)
	if err := categories.Create(ctx, second); err != nil {
		t.Fatal(err)
	}
	if err := categories.Create(ctx, first); err != nil {
		t.Fatal(err)
	}
	if err := categories.Create(ctx, categoryFixture(srvB.ID, "Other server", 0)); err != nil {
		t.Fatal(err)
	}

	got, err := categories.ListByServer(ctx, srvA.ID)
	if err != nil {
		t.Fatalf("ListByServer: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d categories, want 2", len(got))
	}
	if got[0].Name != "First" || got[1].Name != "Second" {
		t.Errorf("order = [%s, %s], want [First, Second]", got[0].Name, got[1].Name)
	}
}

func TestCategoryRepo_Update_ChangesName(t *testing.T) {
	categories, servers, _ := newRepo(t)
	ctx := context.Background()
	srv, chans := serverFixture("owner-" + uuid.NewString())
	if err := servers.CreateServer(ctx, srv, nil, chans); err != nil {
		t.Fatalf("seed server: %v", err)
	}
	cat := categoryFixture(srv.ID, "old-name", 0)
	if err := categories.Create(ctx, cat); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := categories.Update(ctx, cat.ID, "new-name"); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, err := categories.GetByID(ctx, cat.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Name != "new-name" {
		t.Errorf("got.Name = %q, want new-name", got.Name)
	}
}

func TestCategoryRepo_Update_NotFound_ErrCategoryNotFound(t *testing.T) {
	categories, _, _ := newRepo(t)
	err := categories.Update(context.Background(), uuid.New(), "name")
	if !errors.Is(err, model.ErrCategoryNotFound) {
		t.Errorf("err = %v, want ErrCategoryNotFound", err)
	}
}

func TestCategoryRepo_ReorderPositions_UpdatesInOrder(t *testing.T) {
	categories, servers, _ := newRepo(t)
	ctx := context.Background()
	srv, chans := serverFixture("owner-" + uuid.NewString())
	if err := servers.CreateServer(ctx, srv, nil, chans); err != nil {
		t.Fatalf("seed server: %v", err)
	}

	a := categoryFixture(srv.ID, "A", 0)
	b := categoryFixture(srv.ID, "B", 1)
	if err := categories.Create(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err := categories.Create(ctx, b); err != nil {
		t.Fatal(err)
	}

	if err := categories.ReorderPositions(ctx, []uuid.UUID{b.ID, a.ID}); err != nil {
		t.Fatalf("ReorderPositions: %v", err)
	}

	got, err := categories.ListByServer(ctx, srv.ID)
	if err != nil {
		t.Fatalf("ListByServer: %v", err)
	}
	if got[0].Name != "B" || got[1].Name != "A" {
		t.Errorf("order = [%s, %s], want [B, A]", got[0].Name, got[1].Name)
	}
}

func dropAllTestTables(ctx context.Context, pool *pgxpool.Pool) {
	_, _ = pool.Exec(ctx, `DROP TABLE IF EXISTS member_roles, roles, server_bans, invitations, server_members, categories, channels, servers CASCADE`)
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
