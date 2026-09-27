package channel_repo_test

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
	"github.com/discordia-grupo01/servers/internal/repository/channel_repo"
	"github.com/discordia-grupo01/servers/internal/service"
)

// testOutboxEvent is a valid placeholder for CreateServer's outbox event in
// tests that don't care about its content, only that the call as a whole
// succeeds -- payload can't be nil (outbox_events.payload is NOT NULL).
func testOutboxEvent() outbox.Event {
	return outbox.Event{RoutingKey: "test.event", Payload: []byte("{}")}
}

func newRepo(t *testing.T) (*channel_repo.ChannelRepository, *category_repo.CategoryRepository, *repository.ServerRepository) {
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
	} {
		schema, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read migration %s: %v", file, err)
		}
		if _, err := pool.Exec(ctx, string(schema)); err != nil {
			t.Fatalf("apply migration %s: %v", file, err)
		}
	}
	return channel_repo.NewChannelRepository(pool), category_repo.NewCategoryRepository(pool), repository.NewServerRepository(pool)
}

func serverFixture(owner string) (model.Server, []model.Channel) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	id := uuid.New()
	return model.Server{ID: id, Name: "Test Server", NameKey: "test server", OwnerID: owner, CreatedAt: now}, nil
}

func channelFixture(serverID uuid.UUID, name string, categoryID *uuid.UUID) model.Channel {
	return model.Channel{
		ID: uuid.New(), ServerID: serverID, Name: name, Kind: model.ChannelText,
		CategoryID: categoryID, CreatedAt: time.Now().UTC().Truncate(time.Millisecond),
	}
}

func TestChannelRepo_Create_ThenGetByID_ReturnsIt(t *testing.T) {
	channels, _, servers := newRepo(t)
	ctx := context.Background()
	srv, chans := serverFixture("owner-" + uuid.NewString())
	if err := servers.CreateServer(ctx, srv, nil, chans, testOutboxEvent()); err != nil {
		t.Fatalf("seed server: %v", err)
	}

	ch := channelFixture(srv.ID, "general", nil)
	if err := channels.Create(ctx, ch); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := channels.GetByID(ctx, ch.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Name != "general" || got.CategoryID != nil || got.Topic != nil {
		t.Errorf("got = %+v", got)
	}
}

func TestChannelRepo_GetByID_NotFound_ReturnsErrChannelNotFound(t *testing.T) {
	channels, _, _ := newRepo(t)
	_, err := channels.GetByID(context.Background(), uuid.New())
	if !errors.Is(err, model.ErrChannelNotFound) {
		t.Errorf("err = %v, want ErrChannelNotFound", err)
	}
}

func TestChannelRepo_Create_DuplicateNameNoCategory_ErrChannelNameTaken(t *testing.T) {
	channels, _, servers := newRepo(t)
	ctx := context.Background()
	srv, chans := serverFixture("owner-" + uuid.NewString())
	if err := servers.CreateServer(ctx, srv, nil, chans, testOutboxEvent()); err != nil {
		t.Fatalf("seed server: %v", err)
	}

	if err := channels.Create(ctx, channelFixture(srv.ID, "general", nil)); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	err := channels.Create(ctx, channelFixture(srv.ID, "general", nil))
	if !errors.Is(err, model.ErrChannelNameTaken) {
		t.Errorf("err = %v, want ErrChannelNameTaken", err)
	}
}

func TestChannelRepo_Create_DuplicateNameSameCategory_ErrChannelNameTaken(t *testing.T) {
	channels, categories, servers := newRepo(t)
	ctx := context.Background()
	srv, chans := serverFixture("owner-" + uuid.NewString())
	if err := servers.CreateServer(ctx, srv, nil, chans, testOutboxEvent()); err != nil {
		t.Fatalf("seed server: %v", err)
	}
	cat := model.Category{ID: uuid.New(), ServerID: srv.ID, Name: "Texto", NameKey: service.NameKey("Texto"), CreatedAt: time.Now().UTC().Truncate(time.Millisecond)}
	if err := categories.Create(ctx, cat); err != nil {
		t.Fatalf("seed category: %v", err)
	}

	if err := channels.Create(ctx, channelFixture(srv.ID, "anuncios", &cat.ID)); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	err := channels.Create(ctx, channelFixture(srv.ID, "anuncios", &cat.ID))
	if !errors.Is(err, model.ErrChannelNameTaken) {
		t.Errorf("err = %v, want ErrChannelNameTaken", err)
	}
}

func TestChannelRepo_Create_SameNameDifferentCategory_Allowed(t *testing.T) {
	channels, categories, servers := newRepo(t)
	ctx := context.Background()
	srv, chans := serverFixture("owner-" + uuid.NewString())
	if err := servers.CreateServer(ctx, srv, nil, chans, testOutboxEvent()); err != nil {
		t.Fatalf("seed server: %v", err)
	}
	catA := model.Category{ID: uuid.New(), ServerID: srv.ID, Name: "A", NameKey: service.NameKey("A"), CreatedAt: time.Now().UTC().Truncate(time.Millisecond)}
	catB := model.Category{ID: uuid.New(), ServerID: srv.ID, Name: "B", NameKey: service.NameKey("B"), CreatedAt: time.Now().UTC().Truncate(time.Millisecond)}
	if err := categories.Create(ctx, catA); err != nil {
		t.Fatal(err)
	}
	if err := categories.Create(ctx, catB); err != nil {
		t.Fatal(err)
	}

	if err := channels.Create(ctx, channelFixture(srv.ID, "general", &catA.ID)); err != nil {
		t.Fatalf("Create in catA: %v", err)
	}
	if err := channels.Create(ctx, channelFixture(srv.ID, "general", &catB.ID)); err != nil {
		t.Errorf("Create in catB should be allowed, got: %v", err)
	}
}

func TestChannelRepo_Update_ChangesNameAndTopic(t *testing.T) {
	channels, _, servers := newRepo(t)
	ctx := context.Background()
	srv, chans := serverFixture("owner-" + uuid.NewString())
	if err := servers.CreateServer(ctx, srv, nil, chans, testOutboxEvent()); err != nil {
		t.Fatalf("seed server: %v", err)
	}
	ch := channelFixture(srv.ID, "general", nil)
	if err := channels.Create(ctx, ch); err != nil {
		t.Fatalf("Create: %v", err)
	}

	topic := "new topic"
	if err := channels.Update(ctx, ch.ID, "renamed", &topic); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, err := channels.GetByID(ctx, ch.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Name != "renamed" || got.Topic == nil || *got.Topic != topic {
		t.Errorf("got = %+v", got)
	}
}

func TestChannelRepo_Update_NotFound_ErrChannelNotFound(t *testing.T) {
	channels, _, _ := newRepo(t)
	err := channels.Update(context.Background(), uuid.New(), "name", nil)
	if !errors.Is(err, model.ErrChannelNotFound) {
		t.Errorf("err = %v, want ErrChannelNotFound", err)
	}
}

func TestChannelRepo_Delete_RemovesIt(t *testing.T) {
	channels, _, servers := newRepo(t)
	ctx := context.Background()
	srv, chans := serverFixture("owner-" + uuid.NewString())
	if err := servers.CreateServer(ctx, srv, nil, chans, testOutboxEvent()); err != nil {
		t.Fatalf("seed server: %v", err)
	}
	ch := channelFixture(srv.ID, "general", nil)
	if err := channels.Create(ctx, ch); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := channels.Delete(ctx, ch.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := channels.GetByID(ctx, ch.ID); !errors.Is(err, model.ErrChannelNotFound) {
		t.Errorf("channel should be gone, got err=%v", err)
	}
}

func TestChannelRepo_Delete_NotFound_ErrChannelNotFound(t *testing.T) {
	channels, _, _ := newRepo(t)
	err := channels.Delete(context.Background(), uuid.New())
	if !errors.Is(err, model.ErrChannelNotFound) {
		t.Errorf("err = %v, want ErrChannelNotFound", err)
	}
}

func TestChannelRepo_MoveToCategory_UpdatesCategoryID(t *testing.T) {
	channels, categories, servers := newRepo(t)
	ctx := context.Background()
	srv, chans := serverFixture("owner-" + uuid.NewString())
	if err := servers.CreateServer(ctx, srv, nil, chans, testOutboxEvent()); err != nil {
		t.Fatalf("seed server: %v", err)
	}
	cat := model.Category{ID: uuid.New(), ServerID: srv.ID, Name: "Texto", NameKey: service.NameKey("Texto"), CreatedAt: time.Now().UTC().Truncate(time.Millisecond)}
	if err := categories.Create(ctx, cat); err != nil {
		t.Fatalf("seed category: %v", err)
	}
	ch := channelFixture(srv.ID, "general", nil)
	if err := channels.Create(ctx, ch); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := channels.MoveToCategory(ctx, ch.ID, &cat.ID); err != nil {
		t.Fatalf("MoveToCategory: %v", err)
	}
	got, err := channels.GetByID(ctx, ch.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.CategoryID == nil || *got.CategoryID != cat.ID {
		t.Errorf("got.CategoryID = %v, want %v", got.CategoryID, cat.ID)
	}

	if err := channels.MoveToCategory(ctx, ch.ID, nil); err != nil {
		t.Fatalf("MoveToCategory back to nil: %v", err)
	}
	got, err = channels.GetByID(ctx, ch.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.CategoryID != nil {
		t.Errorf("got.CategoryID = %v, want nil", got.CategoryID)
	}
}

func TestChannelRepo_ListByServerAndCategory_ScopesCorrectly(t *testing.T) {
	channels, categories, servers := newRepo(t)
	ctx := context.Background()
	srv, chans := serverFixture("owner-" + uuid.NewString())
	if err := servers.CreateServer(ctx, srv, nil, chans, testOutboxEvent()); err != nil {
		t.Fatalf("seed server: %v", err)
	}
	cat := model.Category{ID: uuid.New(), ServerID: srv.ID, Name: "Texto", NameKey: service.NameKey("Texto"), CreatedAt: time.Now().UTC().Truncate(time.Millisecond)}
	if err := categories.Create(ctx, cat); err != nil {
		t.Fatalf("seed category: %v", err)
	}

	inCat := channelFixture(srv.ID, "in-cat", &cat.ID)
	noCat := channelFixture(srv.ID, "no-cat", nil)
	if err := channels.Create(ctx, inCat); err != nil {
		t.Fatal(err)
	}
	if err := channels.Create(ctx, noCat); err != nil {
		t.Fatal(err)
	}

	got, err := channels.ListByServerAndCategory(ctx, srv.ID, &cat.ID)
	if err != nil {
		t.Fatalf("ListByServerAndCategory(cat): %v", err)
	}
	if len(got) != 1 || got[0].ID != inCat.ID {
		t.Errorf("got = %+v, want only inCat", got)
	}

	got, err = channels.ListByServerAndCategory(ctx, srv.ID, nil)
	if err != nil {
		t.Fatalf("ListByServerAndCategory(nil): %v", err)
	}
	if len(got) != 1 || got[0].ID != noCat.ID {
		t.Errorf("got = %+v, want only noCat", got)
	}
}

func TestChannelRepo_ReorderPositions_UpdatesInOrder(t *testing.T) {
	channels, _, servers := newRepo(t)
	ctx := context.Background()
	srv, chans := serverFixture("owner-" + uuid.NewString())
	if err := servers.CreateServer(ctx, srv, nil, chans, testOutboxEvent()); err != nil {
		t.Fatalf("seed server: %v", err)
	}
	a := channelFixture(srv.ID, "a", nil)
	b := channelFixture(srv.ID, "b", nil)
	if err := channels.Create(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err := channels.Create(ctx, b); err != nil {
		t.Fatal(err)
	}

	if err := channels.ReorderPositions(ctx, []uuid.UUID{b.ID, a.ID}); err != nil {
		t.Fatalf("ReorderPositions: %v", err)
	}

	got, err := channels.ListByServerAndCategory(ctx, srv.ID, nil)
	if err != nil {
		t.Fatalf("ListByServerAndCategory: %v", err)
	}
	if len(got) != 2 || got[0].ID != b.ID || got[1].ID != a.ID {
		t.Errorf("order = %+v, want [b, a]", got)
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
