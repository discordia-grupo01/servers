package ownership_transfer_repo_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/discordia-grupo01/events-contract/ownership"
	"github.com/discordia-grupo01/events-contract/validate"
)

func transferEvents(ctx context.Context, t *testing.T, pool *pgxpool.Pool, transferID uuid.UUID) []ownership.TransferUpdated {
	t.Helper()
	rows, err := pool.Query(ctx,
		`SELECT payload FROM outbox_events WHERE routing_key = $1 AND payload->>'transfer_id' = $2 ORDER BY created_at, id`,
		ownership.RoutingKeyTransferUpdated, transferID.String(),
	)
	if err != nil {
		t.Fatalf("read outbox: %v", err)
	}
	defer rows.Close()

	var events []ownership.TransferUpdated
	for rows.Next() {
		var payload []byte
		if err := rows.Scan(&payload); err != nil {
			t.Fatalf("scan outbox: %v", err)
		}
		if err := validate.Validate(ownership.RoutingKeyTransferUpdated, payload); err != nil {
			t.Errorf("the event does not meet the contract: %v", err)
		}
		var event ownership.TransferUpdated
		if err := json.Unmarshal(payload, &event); err != nil {
			t.Fatalf("decode %s: %v", payload, err)
		}
		events = append(events, event)
	}
	return events
}

func statuses(events []ownership.TransferUpdated) []string {
	out := make([]string, len(events))
	for i, e := range events {
		out[i] = e.Status
	}
	return out
}

func wantStatuses(t *testing.T, events []ownership.TransferUpdated, want ...string) {
	t.Helper()
	got := statuses(events)
	if len(got) != len(want) {
		t.Fatalf("statuses = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("statuses = %v, want %v", got, want)
		}
	}
}

func TestOwnershipTransferEvents_CreatePublishesPending(t *testing.T) {
	repo, servers, pool := newRepo(t)
	ctx := context.Background()
	srv, member := seedServerWithMember(ctx, t, servers, pool)
	transfer := pendingTransfer(srv, member)

	if err := repo.Create(ctx, transfer); err != nil {
		t.Fatalf("Create: %v", err)
	}

	events := transferEvents(ctx, t, pool, transfer.ID)
	wantStatuses(t, events, "pending")
	got := events[0]
	if got.ServerID != srv.ID.String() || got.FromUserID != srv.OwnerID || got.ToUserID != member {
		t.Errorf("event = %+v, want server=%v from=%v to=%v", got, srv.ID, srv.OwnerID, member)
	}
	if got.ResolvedAt != nil {
		t.Error("a pending transfer must not carry resolved_at")
	}
	if !got.OccurredAt.Equal(transfer.CreatedAt) {
		t.Errorf("occurred_at = %v, want the creation time %v", got.OccurredAt, transfer.CreatedAt)
	}
}

func TestOwnershipTransferEvents_RejectedCancelledAndExpiredAreDistinct(t *testing.T) {
	repo, servers, pool := newRepo(t)
	ctx := context.Background()
	at := time.Now().UTC().Truncate(time.Millisecond)

	srv, member := seedServerWithMember(ctx, t, servers, pool)
	rejected := pendingTransfer(srv, member)
	if err := repo.Create(ctx, rejected); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := repo.Reject(ctx, rejected.ID, at); err != nil {
		t.Fatalf("Reject: %v", err)
	}
	events := transferEvents(ctx, t, pool, rejected.ID)
	wantStatuses(t, events, "pending", "rejected")
	if events[1].ResolvedAt == nil || !events[1].OccurredAt.Equal(at) {
		t.Errorf("the resolution must carry resolved_at and occur at %v: %+v", at, events[1])
	}

	cancelled := pendingTransfer(srv, member)
	if err := repo.Create(ctx, cancelled); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := repo.Cancel(ctx, cancelled.ID, at); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	wantStatuses(t, transferEvents(ctx, t, pool, cancelled.ID), "pending", "cancelled")

	expired := pendingTransfer(srv, member)
	if err := repo.Create(ctx, expired); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := repo.Expire(ctx, expired.ID, at); err != nil {
		t.Fatalf("Expire: %v", err)
	}
	wantStatuses(t, transferEvents(ctx, t, pool, expired.ID), "pending", "expired")
}

func TestOwnershipTransferEvents_AcceptPublishesAccepted(t *testing.T) {
	repo, servers, pool := newRepo(t)
	ctx := context.Background()
	srv, member := seedServerWithMember(ctx, t, servers, pool)
	transfer := pendingTransfer(srv, member)
	if err := repo.Create(ctx, transfer); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if _, err := repo.Accept(ctx, transfer.ID, time.Now().UTC()); err != nil {
		t.Fatalf("Accept: %v", err)
	}

	wantStatuses(t, transferEvents(ctx, t, pool, transfer.ID), "pending", "accepted")
}

func TestOwnershipTransferEvents_FailedResolutionPublishesNothing(t *testing.T) {
	repo, servers, pool := newRepo(t)
	ctx := context.Background()
	srv, member := seedServerWithMember(ctx, t, servers, pool)
	transfer := pendingTransfer(srv, member)
	if err := repo.Create(ctx, transfer); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := repo.Reject(ctx, transfer.ID, time.Now().UTC()); err != nil {
		t.Fatalf("Reject: %v", err)
	}

	if _, err := repo.Accept(ctx, transfer.ID, time.Now().UTC()); err == nil {
		t.Fatal("Accept on a resolved transfer must fail")
	}
	if _, err := repo.Cancel(ctx, transfer.ID, time.Now().UTC()); err == nil {
		t.Fatal("Cancel on a resolved transfer must fail")
	}

	wantStatuses(t, transferEvents(ctx, t, pool, transfer.ID), "pending", "rejected")
}

func TestOwnershipTransferEvents_SweepPublishesExpiredForEachStaleOne(t *testing.T) {
	repo, servers, pool := newRepo(t)
	ctx := context.Background()
	srv, member := seedServerWithMember(ctx, t, servers, pool)

	old := pendingTransfer(srv, member)
	old.CreatedAt = time.Now().UTC().Add(-25 * time.Hour).Truncate(time.Millisecond)
	if err := repo.Create(ctx, old); err != nil {
		t.Fatalf("Create: %v", err)
	}

	now := time.Now().UTC()
	n, err := repo.ExpireStalePending(ctx, now.Add(-24*time.Hour), now)
	if err != nil || n != 1 {
		t.Fatalf("ExpireStalePending = %d, %v, want 1 and no error", n, err)
	}

	wantStatuses(t, transferEvents(ctx, t, pool, old.ID), "pending", "expired")
}
