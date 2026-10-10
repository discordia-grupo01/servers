package word_filter_repo_test

import (
	"context"
	"errors"
	"os"
	"reflect"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/outbox"
	"github.com/discordia-grupo01/servers/internal/repository"
	"github.com/discordia-grupo01/servers/internal/repository/word_filter_repo"
)

func newRepo(t *testing.T) (*word_filter_repo.Repository, *pgxpool.Pool) {
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
		"../../../migrations/0024_server_banned_words.up.sql",
	} {
		schema, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read migration %s: %v", file, err)
		}
		if _, err := pool.Exec(ctx, string(schema)); err != nil {
			t.Fatalf("apply migration %s: %v", file, err)
		}
	}
	return word_filter_repo.NewRepository(pool), pool
}

func dropTables(ctx context.Context, pool *pgxpool.Pool) {
	_, _ = pool.Exec(ctx, `DROP TABLE IF EXISTS server_banned_words, outbox_events, server_members, channels, servers CASCADE`)
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

func event() outbox.Event {
	return outbox.Event{RoutingKey: "servers.word_filter_updated", Payload: []byte(`{"n":1}`)}
}

func countEvents(ctx context.Context, t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM outbox_events WHERE routing_key = 'servers.word_filter_updated'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestReplace_SavesTheListInOrderAndWritesTheEvent(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	serverID := seedServer(ctx, t, pool)

	if err := repo.Replace(ctx, serverID, []string{"zeta", "alfa", "no sirve"}, event()); err != nil {
		t.Fatalf("Replace: %v", err)
	}

	got, err := repo.List(ctx, serverID)
	if err != nil || !reflect.DeepEqual(got, []string{"zeta", "alfa", "no sirve"}) {
		t.Fatalf("List = %q, %v", got, err)
	}
	if n := countEvents(ctx, t, pool); n != 1 {
		t.Errorf("outbox has %d events, want 1", n)
	}
}

func TestReplace_ReplacesThePreviousListAndAnEmptyOneClearsIt(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	serverID := seedServer(ctx, t, pool)
	other := seedServer(ctx, t, pool)
	_ = repo.Replace(ctx, other, []string{"ajena"}, event())

	_ = repo.Replace(ctx, serverID, []string{"uno", "dos"}, event())
	if err := repo.Replace(ctx, serverID, []string{"dos", "tres"}, event()); err != nil {
		t.Fatalf("Replace: %v", err)
	}
	if got, _ := repo.List(ctx, serverID); !reflect.DeepEqual(got, []string{"dos", "tres"}) {
		t.Errorf("after replace = %q", got)
	}

	if err := repo.Replace(ctx, serverID, []string{}, event()); err != nil {
		t.Fatalf("Replace empty: %v", err)
	}
	if got, _ := repo.List(ctx, serverID); len(got) != 0 {
		t.Errorf("after clearing = %q", got)
	}
	if got, _ := repo.List(ctx, other); !reflect.DeepEqual(got, []string{"ajena"}) {
		t.Errorf("another server's list changed: %q", got)
	}
}

func TestReplace_UnknownServerWritesNothing(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()

	err := repo.Replace(ctx, uuid.New(), []string{"x"}, event())

	if !errors.Is(err, model.ErrServerNotFound) {
		t.Fatalf("err = %v, want ErrServerNotFound", err)
	}
	if n := countEvents(ctx, t, pool); n != 0 {
		t.Errorf("outbox has %d events, want 0", n)
	}
}

func TestReplace_ConcurrentSavesNeverMixLists(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	serverID := seedServer(ctx, t, pool)
	lists := [][]string{{"a1", "a2", "a3"}, {"b1", "b2"}, {"c1"}, {"d1", "d2", "d3", "d4"}}

	var wg sync.WaitGroup
	for _, words := range lists {
		wg.Add(1)
		go func(words []string) {
			defer wg.Done()
			if err := repo.Replace(ctx, serverID, words, event()); err != nil {
				t.Errorf("Replace: %v", err)
			}
		}(words)
	}
	wg.Wait()

	got, _ := repo.List(ctx, serverID)
	matches := false
	for _, words := range lists {
		matches = matches || reflect.DeepEqual(got, words)
	}
	if !matches {
		t.Errorf("final list %q is a mix of several saves", got)
	}
	if n := countEvents(ctx, t, pool); n != len(lists) {
		t.Errorf("outbox has %d events, want %d", n, len(lists))
	}
}

func TestDeletingTheServerTakesItsList(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	serverID := seedServer(ctx, t, pool)
	_ = repo.Replace(ctx, serverID, []string{"x"}, event())

	if _, err := pool.Exec(ctx, `DELETE FROM servers WHERE id = $1`, serverID); err != nil {
		t.Fatal(err)
	}
	var n int
	_ = pool.QueryRow(ctx, `SELECT COUNT(*) FROM server_banned_words`).Scan(&n)
	if n != 0 {
		t.Errorf("%d words survived the server", n)
	}
}
