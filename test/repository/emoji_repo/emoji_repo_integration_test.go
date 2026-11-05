package emoji_repo_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/model/emoji"
	"github.com/discordia-grupo01/servers/internal/outbox"
	"github.com/discordia-grupo01/servers/internal/repository"
	"github.com/discordia-grupo01/servers/internal/repository/emoji_repo"
)

func newRepo(t *testing.T) (*emoji_repo.Repository, *pgxpool.Pool) {
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
	t.Cleanup(func() { dropTables(ctx, pool) })

	dropTables(ctx, pool)
	for _, file := range []string{
		"../../../migrations/0001_init.up.sql",
		"../../../migrations/0011_outbox_events.up.sql",
		"../../../migrations/0030_server_emojis.up.sql",
		"../../../migrations/0031_server_boosts.up.sql",
	} {
		schema, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read migration %s: %v", file, err)
		}
		if _, err := pool.Exec(ctx, string(schema)); err != nil {
			t.Fatalf("apply migration %s: %v", file, err)
		}
	}
	return emoji_repo.NewRepository(pool), pool
}

func dropTables(ctx context.Context, pool *pgxpool.Pool) {
	_, _ = pool.Exec(ctx, `DROP TABLE IF EXISTS server_boosts, server_emojis, outbox_events, server_members, channels, servers CASCADE`)
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func seedServer(ctx context.Context, t *testing.T, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO servers (id, name, name_key, owner_id) VALUES ($1, 'Server', $2, 'owner-1')`, id, "s-"+id.String()); err != nil {
		t.Fatal(err)
	}
	return id
}

func newEmoji(serverID uuid.UUID, name string) emoji.Emoji {
	return emoji.Emoji{
		ID: uuid.New(), ServerID: serverID, Name: name, Animated: true,
		ImageURL: "https://cdn.test/" + name + ".gif", CreatedBy: "owner-1", CreatedAt: time.Now().UTC(),
	}
}

func event(key string) outbox.Event {
	return outbox.Event{RoutingKey: key, Payload: []byte(`{}`)}
}

func outboxCount(ctx context.Context, t *testing.T, pool *pgxpool.Pool, key string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM outbox_events WHERE routing_key = $1`, key).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestCreateListAndEvent(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	serverID := seedServer(ctx, t, pool)

	first := newEmoji(serverID, "first")
	if err := repo.Create(ctx, first, 50, event("created")); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	second := newEmoji(serverID, "second")
	second.CreatedAt = first.CreatedAt.Add(time.Second)
	if err := repo.Create(ctx, second, 50, event("created")); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	otherServer := seedServer(ctx, t, pool)
	if err := repo.Create(ctx, newEmoji(otherServer, "first"), 50, event("created")); err != nil {
		t.Fatalf("same name in another server: %v", err)
	}

	got, err := repo.List(ctx, serverID)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(got) != 2 || got[0].ID != first.ID || got[1].ID != second.ID {
		t.Fatalf("got %+v", got)
	}
	if !got[0].Animated || got[0].ImageURL != first.ImageURL || got[0].CreatedBy != "owner-1" || got[0].DeletedAt != nil {
		t.Errorf("first = %+v", got[0])
	}
	if n := outboxCount(ctx, t, pool, "created"); n != 3 {
		t.Errorf("outbox events = %d", n)
	}
}

func TestCreateNameTakenIgnoresCaseAndLeavesNoEvent(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	serverID := seedServer(ctx, t, pool)

	if err := repo.Create(ctx, newEmoji(serverID, "party"), 50, event("created")); err != nil {
		t.Fatal(err)
	}
	err := repo.Create(ctx, newEmoji(serverID, "PARTY"), 50, event("created"))
	if !errors.Is(err, model.ErrEmojiNameTaken) {
		t.Fatalf("error = %v", err)
	}
	if n := outboxCount(ctx, t, pool, "created"); n != 1 {
		t.Errorf("outbox events = %d", n)
	}
}

func TestCreateServerNotFound(t *testing.T) {
	repo, _ := newRepo(t)
	err := repo.Create(context.Background(), newEmoji(uuid.New(), "party"), 50, event("created"))
	if !errors.Is(err, model.ErrServerNotFound) {
		t.Errorf("error = %v", err)
	}
}

func TestCreateConcurrentUploadsDontExceedTheLimit(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	serverID := seedServer(ctx, t, pool)
	const maxEmojis, attempts = 3, 10

	var wg sync.WaitGroup
	errs := make([]error, attempts)
	for i := range attempts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = repo.Create(ctx, newEmoji(serverID, fmt.Sprintf("emoji_%d", i)), maxEmojis, event("created"))
		}()
	}
	wg.Wait()

	created := 0
	for _, err := range errs {
		switch {
		case err == nil:
			created++
		case !errors.Is(err, model.ErrEmojiLimitReached):
			t.Errorf("unexpected error: %v", err)
		}
	}
	if created != maxEmojis {
		t.Errorf("created = %d, want %d", created, maxEmojis)
	}
}

func TestDeleteKeepsTheRowAndFreesNameAndSlot(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	serverID := seedServer(ctx, t, pool)
	party := newEmoji(serverID, "party")
	if err := repo.Create(ctx, party, 1, event("created")); err != nil {
		t.Fatal(err)
	}

	var built emoji.Emoji
	at := time.Now().UTC()
	err := repo.Delete(ctx, serverID, party.ID, "mod-1", at, func(e emoji.Emoji) (outbox.Event, error) {
		built = e
		return event("deleted"), nil
	})
	if err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if built.ID != party.ID || built.ImageURL != party.ImageURL || built.DeletedAt == nil || built.DeletedBy == nil || *built.DeletedBy != "mod-1" {
		t.Errorf("built = %+v", built)
	}
	if n := outboxCount(ctx, t, pool, "deleted"); n != 1 {
		t.Errorf("outbox events = %d", n)
	}

	listed, err := repo.List(ctx, serverID)
	if err != nil || len(listed) != 0 {
		t.Errorf("listed %+v, err %v", listed, err)
	}
	var rows int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM server_emojis WHERE id = $1`, party.ID).Scan(&rows); err != nil || rows != 1 {
		t.Errorf("rows = %d, err %v", rows, err)
	}

	if err := repo.Create(ctx, newEmoji(serverID, "party"), 1, event("created")); err != nil {
		t.Errorf("name and slot should be free: %v", err)
	}
}

func TestDeleteNotFound(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	serverID := seedServer(ctx, t, pool)
	otherServer := seedServer(ctx, t, pool)
	party := newEmoji(serverID, "party")
	if err := repo.Create(ctx, party, 50, event("created")); err != nil {
		t.Fatal(err)
	}
	build := func(emoji.Emoji) (outbox.Event, error) { return event("deleted"), nil }

	if err := repo.Delete(ctx, otherServer, party.ID, "mod-1", time.Now(), build); !errors.Is(err, model.ErrEmojiNotFound) {
		t.Errorf("another server's emoji: %v", err)
	}
	if err := repo.Delete(ctx, serverID, party.ID, "mod-1", time.Now(), build); err != nil {
		t.Fatal(err)
	}
	if err := repo.Delete(ctx, serverID, party.ID, "mod-1", time.Now(), build); !errors.Is(err, model.ErrEmojiNotFound) {
		t.Errorf("already deleted: %v", err)
	}
	if n := outboxCount(ctx, t, pool, "deleted"); n != 1 {
		t.Errorf("outbox events = %d", n)
	}
}

func TestDeleteRollsBackWhenTheEventFails(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	serverID := seedServer(ctx, t, pool)
	party := newEmoji(serverID, "party")
	if err := repo.Create(ctx, party, 50, event("created")); err != nil {
		t.Fatal(err)
	}
	boom := errors.New("boom")
	err := repo.Delete(ctx, serverID, party.ID, "mod-1", time.Now(), func(emoji.Emoji) (outbox.Event, error) {
		return outbox.Event{}, boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("error = %v", err)
	}
	listed, err := repo.List(ctx, serverID)
	if err != nil || len(listed) != 1 {
		t.Errorf("emoji should stay active: %+v, err %v", listed, err)
	}
}

func TestDeletingTheServerRemovesItsEmojis(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	serverID := seedServer(ctx, t, pool)
	if err := repo.Create(ctx, newEmoji(serverID, "party"), 50, event("created")); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM servers WHERE id = $1`, serverID); err != nil {
		t.Fatal(err)
	}
	listed, err := repo.List(ctx, serverID)
	if err != nil || len(listed) != 0 {
		t.Errorf("listed %+v, err %v", listed, err)
	}
}

func TestSchemaRejectsInvalidRows(t *testing.T) {
	_, pool := newRepo(t)
	ctx := context.Background()
	serverID := seedServer(ctx, t, pool)
	cases := map[string]string{
		"bad name": `INSERT INTO server_emojis (id, server_id, name, animated, image_url, created_by, created_at)
			VALUES (gen_random_uuid(), $1, 'no spaces allowed', false, 'u', 'o', now())`,
		"half deleted": `INSERT INTO server_emojis (id, server_id, name, animated, image_url, created_by, created_at, deleted_at)
			VALUES (gen_random_uuid(), $1, 'party', false, 'u', 'o', now(), now())`,
	}
	for name, sql := range cases {
		if _, err := pool.Exec(ctx, sql, serverID); err == nil {
			t.Errorf("%s: expected a check violation", name)
		}
	}
}
