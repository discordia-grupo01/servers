package repository_test

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
)

func newRepo(t *testing.T) (*repository.ServerRepository, *pgxpool.Pool) {
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

	schema, err := os.ReadFile("../../migrations/0001_init.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	dropAllTestTables(ctx, pool)
	if _, err := pool.Exec(ctx, string(schema)); err != nil {
		t.Fatalf("apply schema: %v", err)
	}
	return repository.NewServerRepository(pool), pool
}

// dropAllTestTables wipes every table any of this repo's integration test
// packages might have created, so a test run leaves the database exactly as
// clean as it found it -- both before it starts and after it finishes.
func dropAllTestTables(ctx context.Context, pool *pgxpool.Pool) {
	_, _ = pool.Exec(ctx, `DROP TABLE IF EXISTS member_roles, roles, server_members, channels, servers CASCADE`)
}

func serverFixture(name, owner string) (model.Server, []model.Channel) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	id := uuid.New()
	srv := model.Server{ID: id, Name: name, NameKey: name, OwnerID: owner, CreatedAt: now}
	chans := []model.Channel{
		{ID: uuid.New(), ServerID: id, Name: "general", Kind: model.ChannelText, CreatedAt: now},
		{ID: uuid.New(), ServerID: id, Name: "General", Kind: model.ChannelVoice, CreatedAt: now},
	}
	return srv, chans
}

func TestRepo_CreateServer_PersistsServerChannelsAndOwnerMembership(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	owner := "owner-" + uuid.NewString()
	srv, chans := serverFixture("Persist Test", owner)

	if err := repo.CreateServer(ctx, srv, chans); err != nil {
		t.Fatalf("CreateServer: %v", err)
	}

	var name, gotOwner string
	var isOwner bool
	if err := pool.QueryRow(ctx,
		`SELECT s.name, m.user_id, m.is_owner
		   FROM servers s JOIN server_members m ON m.server_id = s.id
		  WHERE s.id = $1`, srv.ID,
	).Scan(&name, &gotOwner, &isOwner); err != nil {
		t.Fatalf("select: %v", err)
	}
	if name != "Persist Test" || gotOwner != owner || !isOwner {
		t.Errorf("name=%q owner=%q is_owner=%v", name, gotOwner, isOwner)
	}

	var nChannels int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM channels WHERE server_id = $1`, srv.ID).Scan(&nChannels)
	if nChannels != 2 {
		t.Errorf("channels = %d, want 2", nChannels)
	}
}

func TestRepo_CreateServer_DuplicateIDFails(t *testing.T) {
	repo, _ := newRepo(t)
	ctx := context.Background()
	srv, chans := serverFixture("Dup ID", "owner-"+uuid.NewString())

	if err := repo.CreateServer(ctx, srv, chans); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	if err := repo.CreateServer(ctx, srv, chans); err == nil {
		t.Error("expected an error for a duplicate id")
	}
}

func TestRepo_CreateServer_SameNameKeySameOwnerReturnsNameTaken(t *testing.T) {
	repo, _ := newRepo(t)
	ctx := context.Background()
	owner := "owner-" + uuid.NewString()

	first, firstCh := serverFixture("Comunidad", owner)
	if err := repo.CreateServer(ctx, first, firstCh); err != nil {
		t.Fatalf("first insert: %v", err)
	}

	second, secondCh := serverFixture("COMUNIDAD", owner) // distinto Name/ID
	second.NameKey = first.NameKey                        // mismo name_key
	if err := repo.CreateServer(ctx, second, secondCh); !errors.Is(err, model.ErrNameTaken) {
		t.Fatalf("err = %v, want ErrNameTaken", err)
	}
}

func TestRepo_CreateServer_SameNameKeyDifferentOwnerIsAllowed(t *testing.T) {
	repo, _ := newRepo(t)
	ctx := context.Background()

	a, aCh := serverFixture("Comunidad", "owner-"+uuid.NewString())
	if err := repo.CreateServer(ctx, a, aCh); err != nil {
		t.Fatalf("first insert: %v", err)
	}

	b, bCh := serverFixture("Comunidad", "owner-"+uuid.NewString())
	b.NameKey = a.NameKey
	if err := repo.CreateServer(ctx, b, bCh); err != nil {
		t.Errorf("same name_key / different owner should be allowed: %v", err)
	}
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
