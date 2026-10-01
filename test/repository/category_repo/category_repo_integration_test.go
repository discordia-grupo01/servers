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
	"github.com/discordia-grupo01/servers/internal/outbox"
	"github.com/discordia-grupo01/servers/internal/repository"
	"github.com/discordia-grupo01/servers/internal/repository/category_repo"
	"github.com/discordia-grupo01/servers/internal/service"
)

// testOutboxEvent is a valid placeholder for CreateServer's outbox event in
// tests that don't care about its content, only that the call as a whole
// succeeds -- payload can't be nil (outbox_events.payload is NOT NULL).
func testOutboxEvent() outbox.Event {
	return outbox.Event{RoutingKey: "test.event", Payload: []byte("{}")}
}

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
		"../../../migrations/0011_outbox_events.up.sql",
		"../../../migrations/0012_category_name_key.up.sql",
		"../../../migrations/0013_unassigned_category.up.sql",
		"../../../migrations/0014_drop_default_kind.up.sql",
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
		ID: uuid.New(), ServerID: serverID, Name: name, NameKey: service.NameKey(name),
		Position:  position,
		CreatedAt: time.Now().UTC().Truncate(time.Millisecond),
	}
}

func TestCategoryRepo_Create_ThenGetByID_ReturnsIt(t *testing.T) {
	categories, servers, _ := newRepo(t)
	ctx := context.Background()
	srv, chans := serverFixture("owner-" + uuid.NewString())
	if err := servers.CreateServer(ctx, srv, nil, chans, []outbox.Event{testOutboxEvent()}); err != nil {
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

func TestCategoryRepo_Create_DuplicateNameSameServer_ErrCategoryNameTaken(t *testing.T) {
	categories, servers, _ := newRepo(t)
	ctx := context.Background()
	srv, chans := serverFixture("owner-" + uuid.NewString())
	if err := servers.CreateServer(ctx, srv, nil, chans, []outbox.Event{testOutboxEvent()}); err != nil {
		t.Fatalf("seed server: %v", err)
	}

	if err := categories.Create(ctx, categoryFixture(srv.ID, "Eventos", 0)); err != nil {
		t.Fatalf("Create: %v", err)
	}

	err := categories.Create(ctx, categoryFixture(srv.ID, "Eventos", 1))
	if !errors.Is(err, model.ErrCategoryNameTaken) {
		t.Errorf("err = %v, want ErrCategoryNameTaken", err)
	}
}

func TestCategoryRepo_Create_DuplicateNameKeyDifferentCaseAndAccents_ErrCategoryNameTaken(t *testing.T) {
	categories, servers, _ := newRepo(t)
	ctx := context.Background()
	srv, chans := serverFixture("owner-" + uuid.NewString())
	if err := servers.CreateServer(ctx, srv, nil, chans, []outbox.Event{testOutboxEvent()}); err != nil {
		t.Fatalf("seed server: %v", err)
	}

	if err := categories.Create(ctx, categoryFixture(srv.ID, "Eventós", 0)); err != nil {
		t.Fatalf("Create: %v", err)
	}

	err := categories.Create(ctx, categoryFixture(srv.ID, "  EVENTOS ", 1))
	if !errors.Is(err, model.ErrCategoryNameTaken) {
		t.Errorf("err = %v, want ErrCategoryNameTaken", err)
	}
}

func TestCategoryRepo_Create_SameNameDifferentServer_Succeeds(t *testing.T) {
	categories, servers, _ := newRepo(t)
	ctx := context.Background()
	srvA, chansA := serverFixture("owner-" + uuid.NewString())
	srvB, chansB := serverFixture("owner-" + uuid.NewString())
	if err := servers.CreateServer(ctx, srvA, nil, chansA, []outbox.Event{testOutboxEvent()}); err != nil {
		t.Fatalf("seed server A: %v", err)
	}
	if err := servers.CreateServer(ctx, srvB, nil, chansB, []outbox.Event{testOutboxEvent()}); err != nil {
		t.Fatalf("seed server B: %v", err)
	}

	if err := categories.Create(ctx, categoryFixture(srvA.ID, "Eventos", 0)); err != nil {
		t.Fatalf("Create in server A: %v", err)
	}
	if err := categories.Create(ctx, categoryFixture(srvB.ID, "Eventos", 0)); err != nil {
		t.Errorf("Create in server B should have succeeded, got: %v", err)
	}
}

func TestCategoryRepo_Update_DuplicateName_ErrCategoryNameTaken(t *testing.T) {
	categories, servers, _ := newRepo(t)
	ctx := context.Background()
	srv, chans := serverFixture("owner-" + uuid.NewString())
	if err := servers.CreateServer(ctx, srv, nil, chans, []outbox.Event{testOutboxEvent()}); err != nil {
		t.Fatalf("seed server: %v", err)
	}

	if err := categories.Create(ctx, categoryFixture(srv.ID, "Eventos", 0)); err != nil {
		t.Fatalf("Create: %v", err)
	}
	other := categoryFixture(srv.ID, "Anuncios", 1)
	if err := categories.Create(ctx, other); err != nil {
		t.Fatalf("Create: %v", err)
	}

	err := categories.Update(ctx, other.ID, "Eventos", service.NameKey("Eventos"))
	if !errors.Is(err, model.ErrCategoryNameTaken) {
		t.Errorf("err = %v, want ErrCategoryNameTaken", err)
	}
}

func TestCategoryRepo_Create_WithIsFallback_RoundTripsThroughGetByID(t *testing.T) {
	categories, servers, _ := newRepo(t)
	ctx := context.Background()
	srv, chans := serverFixture("owner-" + uuid.NewString())
	if err := servers.CreateServer(ctx, srv, nil, chans, []outbox.Event{testOutboxEvent()}); err != nil {
		t.Fatalf("seed server: %v", err)
	}

	cat := categoryFixture(srv.ID, "Sin asignar", 0)
	cat.IsFallback = true
	if err := categories.Create(ctx, cat); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := categories.GetByID(ctx, cat.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if !got.IsFallback {
		t.Errorf("IsFallback = false, want true")
	}
}

func TestCategoryRepo_Create_WithoutIsFallback_StaysFalse(t *testing.T) {
	categories, servers, _ := newRepo(t)
	ctx := context.Background()
	srv, chans := serverFixture("owner-" + uuid.NewString())
	if err := servers.CreateServer(ctx, srv, nil, chans, []outbox.Event{testOutboxEvent()}); err != nil {
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
	if got.IsFallback {
		t.Errorf("IsFallback = true, want false")
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
	if err := servers.CreateServer(ctx, srvA, nil, chA, []outbox.Event{testOutboxEvent()}); err != nil {
		t.Fatalf("seed server A: %v", err)
	}
	if err := servers.CreateServer(ctx, srvB, nil, chB, []outbox.Event{testOutboxEvent()}); err != nil {
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
	if err := servers.CreateServer(ctx, srv, nil, chans, []outbox.Event{testOutboxEvent()}); err != nil {
		t.Fatalf("seed server: %v", err)
	}
	cat := categoryFixture(srv.ID, "old-name", 0)
	if err := categories.Create(ctx, cat); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := categories.Update(ctx, cat.ID, "new-name", service.NameKey("new-name")); err != nil {
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
	err := categories.Update(context.Background(), uuid.New(), "name", service.NameKey("name"))
	if !errors.Is(err, model.ErrCategoryNotFound) {
		t.Errorf("err = %v, want ErrCategoryNotFound", err)
	}
}

func TestCategoryRepo_ReorderPositions_UpdatesInOrder(t *testing.T) {
	categories, servers, _ := newRepo(t)
	ctx := context.Background()
	srv, chans := serverFixture("owner-" + uuid.NewString())
	if err := servers.CreateServer(ctx, srv, nil, chans, []outbox.Event{testOutboxEvent()}); err != nil {
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
	_, _ = pool.Exec(ctx, `DROP TABLE IF EXISTS outbox_events, member_roles, roles, server_bans, invitations, server_members, categories, channels, servers CASCADE`)
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
