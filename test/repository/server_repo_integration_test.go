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

	dropAllTestTables(ctx, pool)
	for _, path := range []string{
		"../../migrations/0001_init.up.sql",
		"../../migrations/0003_roles.up.sql",
		"../../migrations/0004_member_roles.up.sql",
		"../../migrations/0005_default_role.up.sql",
		"../../migrations/0007_categories.up.sql",
		"../../migrations/0009_server_banner.up.sql",
	} {
		schema, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read migration %s: %v", path, err)
		}
		if _, err := pool.Exec(ctx, string(schema)); err != nil {
			t.Fatalf("apply schema %s: %v", path, err)
		}
	}
	return repository.NewServerRepository(pool), pool
}

// dropAllTestTables wipes every table any of this repo's integration test
// packages might have created, so a test run leaves the database exactly as
// clean as it found it -- both before it starts and after it finishes.
func dropAllTestTables(ctx context.Context, pool *pgxpool.Pool) {
	_, _ = pool.Exec(ctx, `DROP TABLE IF EXISTS member_roles, roles, server_bans, invitations, server_members, categories, channels, servers CASCADE`)
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

func TestRepo_GetByID_ReturnsServerWithItsChannels(t *testing.T) {
	repo, _ := newRepo(t)
	ctx := context.Background()
	srv, chans := serverFixture("Get By ID", "owner-"+uuid.NewString())
	if err := repo.CreateServer(ctx, srv, chans); err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := repo.GetByID(ctx, srv.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Name != srv.Name || got.OwnerID != srv.OwnerID {
		t.Errorf("got = %+v", got)
	}
	if len(got.Channels) != 2 {
		t.Fatalf("channels = %d, want 2", len(got.Channels))
	}
	kinds := map[model.ChannelKind]bool{}
	for _, ch := range got.Channels {
		kinds[ch.Kind] = true
	}
	if !kinds[model.ChannelText] || !kinds[model.ChannelVoice] {
		t.Errorf("channel kinds = %v, want text+voice", kinds)
	}
}

func TestRepo_GetByID_UnknownID_ReturnsNotFound(t *testing.T) {
	repo, _ := newRepo(t)
	if _, err := repo.GetByID(context.Background(), uuid.New()); !errors.Is(err, model.ErrServerNotFound) {
		t.Errorf("err = %v, want ErrServerNotFound", err)
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

func TestRepo_ListForUser_ReturnsOwnedServersWithChannels(t *testing.T) {
	repo, _ := newRepo(t)
	ctx := context.Background()
	owner := "owner-" + uuid.NewString()

	a, aCh := serverFixture("Server A", owner)
	if err := repo.CreateServer(ctx, a, aCh); err != nil {
		t.Fatalf("create A: %v", err)
	}
	b, bCh := serverFixture("Server B", owner)
	if err := repo.CreateServer(ctx, b, bCh); err != nil {
		t.Fatalf("create B: %v", err)
	}

	got, err := repo.ListForUser(ctx, owner)
	if err != nil {
		t.Fatalf("ListForUser: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d servers, want 2", len(got))
	}
	for _, s := range got {
		if len(s.Channels) != 2 {
			t.Errorf("server %s has %d channels, want 2", s.Name, len(s.Channels))
		}
	}
}

func TestRepo_ListForUser_OnlyReturnsServersTheUserBelongsTo(t *testing.T) {
	repo, _ := newRepo(t)
	ctx := context.Background()

	mine, mineCh := serverFixture("Mine", "owner-"+uuid.NewString())
	if err := repo.CreateServer(ctx, mine, mineCh); err != nil {
		t.Fatalf("create mine: %v", err)
	}
	other, otherCh := serverFixture("Not Mine", "someone-else-"+uuid.NewString())
	if err := repo.CreateServer(ctx, other, otherCh); err != nil {
		t.Fatalf("create other: %v", err)
	}

	got, err := repo.ListForUser(ctx, mine.OwnerID)
	if err != nil {
		t.Fatalf("ListForUser: %v", err)
	}
	if len(got) != 1 || got[0].ID != mine.ID {
		t.Errorf("got = %+v, want only %v", got, mine.ID)
	}
}

func TestRepo_ListForUser_IncludesServersJoinedNotOwned(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()

	srv, ch := serverFixture("Joined Server", "owner-"+uuid.NewString())
	if err := repo.CreateServer(ctx, srv, ch); err != nil {
		t.Fatalf("create: %v", err)
	}

	member := "member-" + uuid.NewString()
	if _, err := pool.Exec(ctx,
		`INSERT INTO server_members (server_id, user_id, is_owner, joined_at) VALUES ($1, $2, FALSE, now())`,
		srv.ID, member,
	); err != nil {
		t.Fatalf("seed membership: %v", err)
	}

	got, err := repo.ListForUser(ctx, member)
	if err != nil {
		t.Fatalf("ListForUser: %v", err)
	}
	if len(got) != 1 || got[0].ID != srv.ID {
		t.Errorf("got = %+v, want only %v", got, srv.ID)
	}
}

func TestRepo_UpdateServer_ChangesNameIconAndBanner(t *testing.T) {
	repo, _ := newRepo(t)
	ctx := context.Background()
	srv, chans := serverFixture("Before Update", "owner-"+uuid.NewString())
	if err := repo.CreateServer(ctx, srv, chans); err != nil {
		t.Fatalf("create: %v", err)
	}

	iconURL, bannerURL := "/v1/servers/"+srv.ID.String()+"/icon", "/v1/servers/"+srv.ID.String()+"/banner"
	if err := repo.UpdateServer(ctx, srv.ID, "After Update", "after update", &iconURL, &bannerURL); err != nil {
		t.Fatalf("UpdateServer: %v", err)
	}

	got, err := repo.GetByID(ctx, srv.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Name != "After Update" || got.NameKey != "after update" {
		t.Errorf("name/name_key = %q/%q", got.Name, got.NameKey)
	}
	if got.IconURL == nil || *got.IconURL != iconURL {
		t.Errorf("icon_url = %v, want %q", got.IconURL, iconURL)
	}
	if got.BannerURL == nil || *got.BannerURL != bannerURL {
		t.Errorf("banner_url = %v, want %q", got.BannerURL, bannerURL)
	}
}

func TestRepo_UpdateServer_BannerNil_ClearsIt(t *testing.T) {
	repo, _ := newRepo(t)
	ctx := context.Background()
	srv, chans := serverFixture("Has Banner", "owner-"+uuid.NewString())
	if err := repo.CreateServer(ctx, srv, chans); err != nil {
		t.Fatalf("create: %v", err)
	}
	bannerURL := "/v1/servers/" + srv.ID.String() + "/banner"
	if err := repo.UpdateServer(ctx, srv.ID, srv.Name, srv.NameKey, srv.IconURL, &bannerURL); err != nil {
		t.Fatalf("set banner: %v", err)
	}

	if err := repo.UpdateServer(ctx, srv.ID, srv.Name, srv.NameKey, srv.IconURL, nil); err != nil {
		t.Fatalf("clear banner: %v", err)
	}

	got, err := repo.GetByID(ctx, srv.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.BannerURL != nil {
		t.Errorf("banner_url = %v, want nil", got.BannerURL)
	}
}

func TestRepo_UpdateServer_UnknownID_ReturnsNotFound(t *testing.T) {
	repo, _ := newRepo(t)
	name := "Name"
	err := repo.UpdateServer(context.Background(), uuid.New(), name, name, nil, nil)
	if !errors.Is(err, model.ErrServerNotFound) {
		t.Errorf("err = %v, want ErrServerNotFound", err)
	}
}

func TestRepo_UpdateServer_NameKeyCollisionWithAnotherOwnedServer_ReturnsNameTaken(t *testing.T) {
	repo, _ := newRepo(t)
	ctx := context.Background()
	owner := "owner-" + uuid.NewString()

	a, aCh := serverFixture("Server A", owner)
	if err := repo.CreateServer(ctx, a, aCh); err != nil {
		t.Fatalf("create A: %v", err)
	}
	b, bCh := serverFixture("Server B", owner)
	if err := repo.CreateServer(ctx, b, bCh); err != nil {
		t.Fatalf("create B: %v", err)
	}

	err := repo.UpdateServer(ctx, b.ID, "Server A", a.NameKey, b.IconURL, nil)
	if !errors.Is(err, model.ErrNameTaken) {
		t.Errorf("err = %v, want ErrNameTaken", err)
	}
}

func TestRepo_ListForUser_NoMemberships_ReturnsEmptyNotNil(t *testing.T) {
	repo, _ := newRepo(t)

	got, err := repo.ListForUser(context.Background(), "nobody-"+uuid.NewString())
	if err != nil {
		t.Fatalf("ListForUser: %v", err)
	}
	if got == nil {
		t.Error("got nil, want an empty (non-nil) slice")
	}
	if len(got) != 0 {
		t.Errorf("got %d servers, want 0", len(got))
	}
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
