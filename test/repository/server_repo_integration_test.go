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
	"github.com/discordia-grupo01/servers/internal/outbox"
	"github.com/discordia-grupo01/servers/internal/repository"
)

// testOutboxEvent is a valid placeholder for CreateServer/RedeemInvitation's
// outbox event in tests that don't care about its content, only that the
// call as a whole succeeds -- payload can't be nil (outbox_events.payload
// is NOT NULL).
func testOutboxEvent() outbox.Event {
	return outbox.Event{RoutingKey: "test.event", Payload: []byte("{}")}
}

// testBuildEvent is RedeemInvitation's buildEvent callback for tests that
// don't care about the resolved server_id or the event content.
func testBuildEvent(uuid.UUID) (outbox.Event, error) {
	return testOutboxEvent(), nil
}

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
		"../../migrations/0010_native_categories.up.sql",
		"../../migrations/0011_outbox_events.up.sql",
		"../../migrations/0033_outbox_traceparent.up.sql",
		"../../migrations/0012_category_name_key.up.sql",
		"../../migrations/0013_unassigned_category.up.sql",
		"../../migrations/0014_drop_default_kind.up.sql",
		"../../migrations/0015_role_positions.up.sql",
		"../../migrations/0019_everyone_role.up.sql",
		"../../migrations/0028_server_moderation.up.sql",
		"../../migrations/0031_server_boosts.up.sql",
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
	_, _ = pool.Exec(ctx, `DROP TABLE IF EXISTS server_boosts, outbox_events, member_roles, roles, server_bans, invitations, server_members, categories, channels, servers CASCADE`)
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

	if err := repo.CreateServer(ctx, srv, nil, chans, []outbox.Event{testOutboxEvent()}); err != nil {
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

func TestRepo_CreateServer_PersistsNativeCategoriesAndChannelCategoryID(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	owner := "owner-" + uuid.NewString()
	now := time.Now().UTC().Truncate(time.Millisecond)
	id := uuid.New()

	textCatID, voiceCatID, unassignedCatID := uuid.New(), uuid.New(), uuid.New()
	srv := model.Server{ID: id, Name: "With Categories", NameKey: "with categories", OwnerID: owner, CreatedAt: now}
	categories := []model.Category{
		{ID: textCatID, ServerID: id, Name: model.DefaultTextCategoryName, NameKey: "canales de texto", Position: 0, CreatedAt: now},
		{ID: voiceCatID, ServerID: id, Name: model.DefaultVoiceCategoryName, NameKey: "canales de voz", Position: 1, CreatedAt: now},
		{ID: unassignedCatID, ServerID: id, Name: model.DefaultUnassignedCategoryName, NameKey: "sin asignar", Position: 2, IsFallback: true, CreatedAt: now},
	}
	chans := []model.Channel{
		{ID: uuid.New(), ServerID: id, Name: "general", Kind: model.ChannelText, CategoryID: &textCatID, CreatedAt: now},
		{ID: uuid.New(), ServerID: id, Name: "General", Kind: model.ChannelVoice, CategoryID: &voiceCatID, CreatedAt: now},
	}

	if err := repo.CreateServer(ctx, srv, categories, chans, []outbox.Event{testOutboxEvent()}); err != nil {
		t.Fatalf("CreateServer: %v", err)
	}

	var nCategories int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM categories WHERE server_id = $1`, id).Scan(&nCategories)
	if nCategories != 3 {
		t.Fatalf("categories = %d, want 3", nCategories)
	}

	got, err := repo.GetByID(ctx, id)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if len(got.Categories) != 3 {
		t.Fatalf("got.Categories = %d, want 3: %+v", len(got.Categories), got.Categories)
	}
	for _, cat := range got.Categories {
		if cat.ID == unassignedCatID && !cat.IsFallback {
			t.Errorf("unassigned category round-tripped with IsFallback = false, want true")
		}
	}
	for _, ch := range got.Channels {
		switch ch.Kind {
		case model.ChannelText:
			if ch.CategoryID == nil || *ch.CategoryID != textCatID {
				t.Errorf("text channel CategoryID = %v, want %s", ch.CategoryID, textCatID)
			}
		case model.ChannelVoice:
			if ch.CategoryID == nil || *ch.CategoryID != voiceCatID {
				t.Errorf("voice channel CategoryID = %v, want %s", ch.CategoryID, voiceCatID)
			}
		}
	}
}

func TestRepo_GetByID_ReturnsServerWithItsChannels(t *testing.T) {
	repo, _ := newRepo(t)
	ctx := context.Background()
	srv, chans := serverFixture("Get By ID", "owner-"+uuid.NewString())
	if err := repo.CreateServer(ctx, srv, nil, chans, []outbox.Event{testOutboxEvent()}); err != nil {
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

	if err := repo.CreateServer(ctx, srv, nil, chans, []outbox.Event{testOutboxEvent()}); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	if err := repo.CreateServer(ctx, srv, nil, chans, []outbox.Event{testOutboxEvent()}); err == nil {
		t.Error("expected an error for a duplicate id")
	}
}

func TestRepo_CreateServer_SameNameKeySameOwnerReturnsNameTaken(t *testing.T) {
	repo, _ := newRepo(t)
	ctx := context.Background()
	owner := "owner-" + uuid.NewString()

	first, firstCh := serverFixture("Comunidad", owner)
	if err := repo.CreateServer(ctx, first, nil, firstCh, []outbox.Event{testOutboxEvent()}); err != nil {
		t.Fatalf("first insert: %v", err)
	}

	second, secondCh := serverFixture("COMUNIDAD", owner) // distinto Name/ID
	second.NameKey = first.NameKey                        // mismo name_key
	if err := repo.CreateServer(ctx, second, nil, secondCh, []outbox.Event{testOutboxEvent()}); !errors.Is(err, model.ErrNameTaken) {
		t.Fatalf("err = %v, want ErrNameTaken", err)
	}
}

func TestRepo_CreateServer_SameNameKeyDifferentOwnerIsAllowed(t *testing.T) {
	repo, _ := newRepo(t)
	ctx := context.Background()

	a, aCh := serverFixture("Comunidad", "owner-"+uuid.NewString())
	if err := repo.CreateServer(ctx, a, nil, aCh, []outbox.Event{testOutboxEvent()}); err != nil {
		t.Fatalf("first insert: %v", err)
	}

	b, bCh := serverFixture("Comunidad", "owner-"+uuid.NewString())
	b.NameKey = a.NameKey
	if err := repo.CreateServer(ctx, b, nil, bCh, []outbox.Event{testOutboxEvent()}); err != nil {
		t.Errorf("same name_key / different owner should be allowed: %v", err)
	}
}

func TestRepo_ListForUser_ReturnsOwnedServersWithChannels(t *testing.T) {
	repo, _ := newRepo(t)
	ctx := context.Background()
	owner := "owner-" + uuid.NewString()

	a, aCh := serverFixture("Server A", owner)
	if err := repo.CreateServer(ctx, a, nil, aCh, []outbox.Event{testOutboxEvent()}); err != nil {
		t.Fatalf("create A: %v", err)
	}
	b, bCh := serverFixture("Server B", owner)
	if err := repo.CreateServer(ctx, b, nil, bCh, []outbox.Event{testOutboxEvent()}); err != nil {
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
	if err := repo.CreateServer(ctx, mine, nil, mineCh, []outbox.Event{testOutboxEvent()}); err != nil {
		t.Fatalf("create mine: %v", err)
	}
	other, otherCh := serverFixture("Not Mine", "someone-else-"+uuid.NewString())
	if err := repo.CreateServer(ctx, other, nil, otherCh, []outbox.Event{testOutboxEvent()}); err != nil {
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

func TestRepo_IsMember_OwnerMemberAndStranger(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()

	owner := "owner-" + uuid.NewString()
	srv, ch := serverFixture("Is Member", owner)
	if err := repo.CreateServer(ctx, srv, nil, ch, []outbox.Event{testOutboxEvent()}); err != nil {
		t.Fatalf("create: %v", err)
	}
	member := "member-" + uuid.NewString()
	if _, err := pool.Exec(ctx,
		`INSERT INTO server_members (server_id, user_id, is_owner, joined_at) VALUES ($1, $2, FALSE, now())`,
		srv.ID, member,
	); err != nil {
		t.Fatalf("seed membership: %v", err)
	}

	for _, c := range []struct {
		user string
		want bool
	}{{owner, true}, {member, true}, {"stranger-" + uuid.NewString(), false}} {
		got, err := repo.IsMember(ctx, srv.ID, c.user)
		if err != nil {
			t.Fatalf("IsMember(%s): %v", c.user, err)
		}
		if got != c.want {
			t.Errorf("IsMember(%s) = %v, want %v", c.user, got, c.want)
		}
	}
}

func TestRepo_IsMember_UnknownServer_ReturnsNotFound(t *testing.T) {
	repo, _ := newRepo(t)
	if _, err := repo.IsMember(context.Background(), uuid.New(), "someone"); !errors.Is(err, model.ErrServerNotFound) {
		t.Errorf("err = %v, want ErrServerNotFound", err)
	}
}

func TestRepo_ListForUser_IncludesServersJoinedNotOwned(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()

	srv, ch := serverFixture("Joined Server", "owner-"+uuid.NewString())
	if err := repo.CreateServer(ctx, srv, nil, ch, []outbox.Event{testOutboxEvent()}); err != nil {
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
	if err := repo.CreateServer(ctx, srv, nil, chans, []outbox.Event{testOutboxEvent()}); err != nil {
		t.Fatalf("create: %v", err)
	}

	iconURL, bannerURL := "/v1/servers/"+srv.ID.String()+"/icon", "/v1/servers/"+srv.ID.String()+"/banner"
	if err := repo.UpdateServer(ctx, srv.ID, "After Update", "after update", &iconURL, &bannerURL, false); err != nil {
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
	if err := repo.CreateServer(ctx, srv, nil, chans, []outbox.Event{testOutboxEvent()}); err != nil {
		t.Fatalf("create: %v", err)
	}
	bannerURL := "/v1/servers/" + srv.ID.String() + "/banner"
	if err := repo.UpdateServer(ctx, srv.ID, srv.Name, srv.NameKey, srv.IconURL, &bannerURL, false); err != nil {
		t.Fatalf("set banner: %v", err)
	}

	if err := repo.UpdateServer(ctx, srv.ID, srv.Name, srv.NameKey, srv.IconURL, nil, false); err != nil {
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
	err := repo.UpdateServer(context.Background(), uuid.New(), name, name, nil, nil, false)
	if !errors.Is(err, model.ErrServerNotFound) {
		t.Errorf("err = %v, want ErrServerNotFound", err)
	}
}

func TestRepo_UpdateServer_NameKeyCollisionWithAnotherOwnedServer_ReturnsNameTaken(t *testing.T) {
	repo, _ := newRepo(t)
	ctx := context.Background()
	owner := "owner-" + uuid.NewString()

	a, aCh := serverFixture("Server A", owner)
	if err := repo.CreateServer(ctx, a, nil, aCh, []outbox.Event{testOutboxEvent()}); err != nil {
		t.Fatalf("create A: %v", err)
	}
	b, bCh := serverFixture("Server B", owner)
	if err := repo.CreateServer(ctx, b, nil, bCh, []outbox.Event{testOutboxEvent()}); err != nil {
		t.Fatalf("create B: %v", err)
	}

	err := repo.UpdateServer(ctx, b.ID, "Server A", a.NameKey, b.IconURL, nil, false)
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

func countServerUpdated(ctx context.Context, t *testing.T, pool *pgxpool.Pool, serverID uuid.UUID) int {
	t.Helper()
	var n int
	err := pool.QueryRow(ctx, `
		SELECT count(*) FROM outbox_events
		 WHERE routing_key = 'servers.server_updated' AND payload->>'server_id' = $1`, serverID.String(),
	).Scan(&n)
	if err != nil {
		t.Fatalf("count server_updated: %v", err)
	}
	return n
}

func TestRepo_UpdateServer_RenamePublishesServerUpdatedWithTheNewName(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	srv, chans := serverFixture("Before Rename", "owner-"+uuid.NewString())
	if err := repo.CreateServer(ctx, srv, nil, chans, nil); err != nil {
		t.Fatalf("create: %v", err)
	}
	if got := countServerUpdated(ctx, t, pool, srv.ID); got != 1 {
		t.Fatalf("server_updated after create = %d, want 1", got)
	}

	if err := repo.UpdateServer(ctx, srv.ID, "After Rename", "after rename", srv.IconURL, nil, false); err != nil {
		t.Fatalf("UpdateServer: %v", err)
	}

	if got := countServerUpdated(ctx, t, pool, srv.ID); got != 2 {
		t.Fatalf("server_updated after rename = %d, want 2", got)
	}
	var name, owner string
	err := pool.QueryRow(ctx, `
		SELECT payload->>'name', payload->>'owner_id' FROM outbox_events
		 WHERE routing_key = 'servers.server_updated' AND payload->>'server_id' = $1
		 ORDER BY created_at DESC, (payload->>'occurred_at') DESC LIMIT 1`, srv.ID.String(),
	).Scan(&name, &owner)
	if err != nil {
		t.Fatalf("read event: %v", err)
	}
	if name != "After Rename" || owner != srv.OwnerID {
		t.Errorf("event name/owner = %q/%q, want After Rename/%s", name, owner, srv.OwnerID)
	}
}

func TestRepo_UpdateServer_IconChangePublishesServerUpdatedWithTheNewIcon(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	srv, chans := serverFixture("Same Name", "owner-"+uuid.NewString())
	if err := repo.CreateServer(ctx, srv, nil, chans, nil); err != nil {
		t.Fatalf("create: %v", err)
	}
	iconURL := "/v1/servers/" + srv.ID.String() + "/icon?v=abc123"

	if err := repo.UpdateServer(ctx, srv.ID, srv.Name, srv.NameKey, &iconURL, nil, false); err != nil {
		t.Fatalf("UpdateServer: %v", err)
	}

	if got := countServerUpdated(ctx, t, pool, srv.ID); got != 2 {
		t.Fatalf("server_updated after icon change = %d, want 2", got)
	}
	var icon string
	err := pool.QueryRow(ctx, `
		SELECT payload->>'icon_url' FROM outbox_events
		 WHERE routing_key = 'servers.server_updated' AND payload->>'server_id' = $1
		 ORDER BY created_at DESC, (payload->>'occurred_at') DESC LIMIT 1`, srv.ID.String(),
	).Scan(&icon)
	if err != nil {
		t.Fatalf("read event: %v", err)
	}
	if icon != iconURL {
		t.Errorf("event icon_url = %q, want %q", icon, iconURL)
	}
}

func TestRepo_UpdateServer_OnlyBannerPublishesNoServerUpdated(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	srv, chans := serverFixture("Same Name", "owner-"+uuid.NewString())
	if err := repo.CreateServer(ctx, srv, nil, chans, nil); err != nil {
		t.Fatalf("create: %v", err)
	}
	bannerURL := "/v1/servers/" + srv.ID.String() + "/banner"

	if err := repo.UpdateServer(ctx, srv.ID, srv.Name, srv.NameKey, srv.IconURL, &bannerURL, false); err != nil {
		t.Fatalf("UpdateServer: %v", err)
	}

	if got := countServerUpdated(ctx, t, pool, srv.ID); got != 1 {
		t.Errorf("server_updated = %d, want only the one from creation", got)
	}
}

func TestRepo_UpdateServer_NameTaken_PublishesNothing(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	owner := "owner-" + uuid.NewString()
	a, aCh := serverFixture("Server A", owner)
	b, bCh := serverFixture("Server B", owner)
	for _, s := range []struct {
		srv   model.Server
		chans []model.Channel
	}{{a, aCh}, {b, bCh}} {
		if err := repo.CreateServer(ctx, s.srv, nil, s.chans, nil); err != nil {
			t.Fatalf("create: %v", err)
		}
	}

	err := repo.UpdateServer(ctx, b.ID, "Server A", a.NameKey, b.IconURL, nil, false)
	if !errors.Is(err, model.ErrNameTaken) {
		t.Fatalf("err = %v, want ErrNameTaken", err)
	}

	if got := countServerUpdated(ctx, t, pool, b.ID); got != 1 {
		t.Errorf("server_updated = %d, want only the one from creation", got)
	}
}

// ListForUser reports when the staff suspended a server, and keeps listing it.
func TestRepo_ListForUser_ReportsSuspendedServers(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	owner := "owner-" + uuid.NewString()

	active, activeCh := serverFixture("Activo", owner)
	suspended, suspendedCh := serverFixture("Suspendido", owner)
	for _, s := range []struct {
		srv model.Server
		ch  []model.Channel
	}{{active, activeCh}, {suspended, suspendedCh}} {
		if err := repo.CreateServer(ctx, s.srv, nil, s.ch, []outbox.Event{testOutboxEvent()}); err != nil {
			t.Fatalf("CreateServer %s: %v", s.srv.Name, err)
		}
	}
	when := time.Date(2026, 11, 4, 12, 30, 0, 0, time.UTC)
	if _, err := pool.Exec(ctx, `UPDATE servers SET suspended_at = $2, moderation_version = 1 WHERE id = $1`, suspended.ID, when); err != nil {
		t.Fatalf("suspend: %v", err)
	}

	got, err := repo.ListForUser(ctx, owner)
	if err != nil {
		t.Fatalf("ListForUser: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d servers, want both (a suspended server stays in the list)", len(got))
	}
	for _, s := range got {
		switch s.ID {
		case suspended.ID:
			if s.SuspendedAt == nil || !s.SuspendedAt.Equal(when) {
				t.Errorf("suspended server: SuspendedAt = %v, want %v", s.SuspendedAt, when)
			}
		case active.ID:
			if s.SuspendedAt != nil {
				t.Errorf("active server: SuspendedAt = %v, want nil", s.SuspendedAt)
			}
		}
	}
}

func TestRepo_VisibleImageURLs_FollowTheBoostLevel(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	srv, chans := serverFixture("Con Imagenes", "owner-"+uuid.NewString())
	if err := repo.CreateServer(ctx, srv, nil, chans, nil); err != nil {
		t.Fatalf("CreateServer: %v", err)
	}
	icon, banner := "/icon?v=1", "/banner?v=1"
	if err := repo.UpdateServer(ctx, srv.ID, srv.Name, srv.NameKey, &icon, &banner, true); err != nil {
		t.Fatalf("UpdateServer: %v", err)
	}

	gotIcon, gotBanner, err := repo.VisibleImageURLs(ctx, srv.ID)
	if err != nil || gotIcon != nil || gotBanner != nil {
		t.Errorf("level 0: icon=%v banner=%v err=%v", gotIcon, gotBanner, err)
	}
	if storedIcon, storedBanner, err := repo.IconURLs(ctx, srv.ID); err != nil || storedIcon == nil || storedBanner == nil {
		t.Errorf("what is stored must stay: icon=%v banner=%v err=%v", storedIcon, storedBanner, err)
	}

	if _, err := pool.Exec(ctx, `UPDATE servers SET boost_level = 2 WHERE id = $1`, srv.ID); err != nil {
		t.Fatal(err)
	}
	gotIcon, gotBanner, err = repo.VisibleImageURLs(ctx, srv.ID)
	if err != nil || gotIcon == nil || gotBanner == nil {
		t.Errorf("level 2: icon=%v banner=%v err=%v", gotIcon, gotBanner, err)
	}

	if _, _, err := repo.VisibleImageURLs(ctx, uuid.New()); !errors.Is(err, model.ErrServerNotFound) {
		t.Errorf("missing server: err = %v", err)
	}
}
