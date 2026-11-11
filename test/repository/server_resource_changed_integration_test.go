package repository_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/discordia-grupo01/events-contract/servers"
	"github.com/discordia-grupo01/events-contract/validate"
	"github.com/discordia-grupo01/servers/internal/outbox"
	"github.com/discordia-grupo01/servers/internal/repository"
)

func publishedOf(ctx context.Context, t *testing.T, pool *pgxpool.Pool, key string) []servers.ResourceChanged {
	t.Helper()
	rows, err := pool.Query(ctx, `SELECT payload FROM outbox_events WHERE routing_key = $1 ORDER BY created_at`, key)
	if err != nil {
		t.Fatalf("read outbox: %v", err)
	}
	defer rows.Close()
	var out []servers.ResourceChanged
	for rows.Next() {
		var payload []byte
		if err := rows.Scan(&payload); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if err := validate.Validate(key, payload); err != nil {
			t.Errorf("the event does not meet the contract: %v", err)
		}
		var event servers.ResourceChanged
		if err := json.Unmarshal(payload, &event); err != nil {
			t.Fatalf("decode: %v", err)
		}
		out = append(out, event)
	}
	return out
}

func countOf(ctx context.Context, t *testing.T, pool *pgxpool.Pool, key string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox_events WHERE routing_key = $1`, key).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

func TestUpdateServer_AnIconChangeTellsTheClientsToAskAgain(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	srv, chans := serverFixture("Same Name", "owner-"+uuid.NewString())
	if err := repo.CreateServer(ctx, srv, nil, chans, []outbox.Event{testOutboxEvent()}); err != nil {
		t.Fatalf("create: %v", err)
	}
	before := countOf(ctx, t, pool, servers.RoutingKeyServerUpdated)

	icon := "/v1/servers/" + srv.ID.String() + "/icon"
	if err := repo.UpdateServer(ctx, srv.ID, srv.Name, srv.NameKey, &icon, nil, false); err != nil {
		t.Fatalf("UpdateServer: %v", err)
	}

	changes := publishedOf(ctx, t, pool, servers.RoutingKeyResourceChanged)
	if len(changes) != 1 || changes[0].ServerID != srv.ID.String() || changes[0].Resource != "server" {
		t.Fatalf("resource_changed = %+v, want one for the server", changes)
	}
	if after := countOf(ctx, t, pool, servers.RoutingKeyServerUpdated); after != before {
		t.Errorf("server_updated grew from %d to %d: an icon change must not publish the name snapshot", before, after)
	}
}

func TestUpdateServer_ARenameAlsoPublishesTheNameSnapshot(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	srv, chans := serverFixture("Old Name", "owner-"+uuid.NewString())
	if err := repo.CreateServer(ctx, srv, nil, chans, []outbox.Event{testOutboxEvent()}); err != nil {
		t.Fatalf("create: %v", err)
	}
	before := countOf(ctx, t, pool, servers.RoutingKeyServerUpdated)

	if err := repo.UpdateServer(ctx, srv.ID, "New Name", "new name", nil, nil, false); err != nil {
		t.Fatalf("UpdateServer: %v", err)
	}

	if after := countOf(ctx, t, pool, servers.RoutingKeyServerUpdated); after != before+1 {
		t.Errorf("server_updated = %d, want %d", after, before+1)
	}
	if n := len(publishedOf(ctx, t, pool, servers.RoutingKeyResourceChanged)); n != 1 {
		t.Errorf("resource_changed events = %d, want 1", n)
	}
}

func TestChangeRecorder_PublishesWhatItIsAskedAndResolvesChannelsAndCategories(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	srv, chans := serverFixture("Recorder Server", "owner-"+uuid.NewString())
	if err := repo.CreateServer(ctx, srv, nil, chans, []outbox.Event{testOutboxEvent()}); err != nil {
		t.Fatalf("create: %v", err)
	}
	recorder := repository.NewChangeRecorder(pool)

	if err := recorder.RecordResourceChanged(ctx, srv.ID, "roles"); err != nil {
		t.Fatalf("RecordResourceChanged: %v", err)
	}
	changes := publishedOf(ctx, t, pool, servers.RoutingKeyResourceChanged)
	if len(changes) != 1 || changes[0].Resource != "roles" || changes[0].ServerID != srv.ID.String() {
		t.Errorf("recorded %+v, want roles of the server", changes)
	}

	got, err := recorder.ServerOfChannel(ctx, chans[0].ID)
	if err != nil || got != srv.ID {
		t.Errorf("ServerOfChannel = %v, %v, want %v", got, err, srv.ID)
	}
	if _, err := recorder.ServerOfChannel(ctx, uuid.New()); err == nil {
		t.Error("an unknown channel must not resolve")
	}
	if _, err := recorder.ServerOfCategory(ctx, uuid.New()); err == nil {
		t.Error("an unknown category must not resolve")
	}
}
