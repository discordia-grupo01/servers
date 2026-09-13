package ownership_transfer_repo_test

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
	"github.com/discordia-grupo01/servers/internal/repository/ownership_transfer_repo"
)

func newRepo(t *testing.T) (*ownership_transfer_repo.Repository, *repository.ServerRepository, *pgxpool.Pool) {
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
		"../../../migrations/0006_ownership_transfers.up.sql",
		"../../../migrations/0008_ownership_transfer_cancelled_status.up.sql",
	} {
		schema, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read migration %s: %v", file, err)
		}
		if _, err := pool.Exec(ctx, string(schema)); err != nil {
			t.Fatalf("apply migration %s: %v", file, err)
		}
	}
	return ownership_transfer_repo.NewRepository(pool), repository.NewServerRepository(pool), pool
}

// seedServerWithMember creates a server (its creator is the owner) plus a
// second, regular member -- a valid target for a transfer.
func seedServerWithMember(ctx context.Context, t *testing.T, servers *repository.ServerRepository, pool *pgxpool.Pool) (model.Server, string) {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Millisecond)
	srv := model.Server{ID: uuid.New(), Name: "Test Server", NameKey: "test server", OwnerID: "owner-" + uuid.NewString(), CreatedAt: now}
	if err := servers.CreateServer(ctx, srv, nil); err != nil {
		t.Fatalf("seed server: %v", err)
	}

	member := "member-" + uuid.NewString()
	if _, err := pool.Exec(ctx,
		`INSERT INTO server_members (server_id, user_id, is_owner, joined_at) VALUES ($1, $2, FALSE, $3)`,
		srv.ID, member, now,
	); err != nil {
		t.Fatalf("seed member: %v", err)
	}
	return srv, member
}

func pendingTransfer(srv model.Server, toUser string) model.OwnershipTransfer {
	return model.OwnershipTransfer{
		ID: uuid.New(), ServerID: srv.ID, FromUserID: srv.OwnerID, ToUserID: toUser,
		Status: model.OwnershipTransferPending, CreatedAt: time.Now().UTC().Truncate(time.Millisecond),
	}
}

func isOwnerFlag(ctx context.Context, t *testing.T, pool *pgxpool.Pool, serverID uuid.UUID, userID string) bool {
	t.Helper()
	var owner bool
	if err := pool.QueryRow(ctx,
		`SELECT is_owner FROM server_members WHERE server_id = $1 AND user_id = $2`, serverID, userID,
	).Scan(&owner); err != nil {
		t.Fatalf("check is_owner for %s: %v", userID, err)
	}
	return owner
}

func serverOwnerID(ctx context.Context, t *testing.T, pool *pgxpool.Pool, serverID uuid.UUID) string {
	t.Helper()
	var ownerID string
	if err := pool.QueryRow(ctx, `SELECT owner_id FROM servers WHERE id = $1`, serverID).Scan(&ownerID); err != nil {
		t.Fatalf("select server owner: %v", err)
	}
	return ownerID
}

func TestOwnershipTransferRepo_CreateAndGetByID_RoundTrips(t *testing.T) {
	repo, servers, pool := newRepo(t)
	ctx := context.Background()
	srv, member := seedServerWithMember(ctx, t, servers, pool)

	want := pendingTransfer(srv, member)
	if err := repo.Create(ctx, want); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.GetByID(ctx, want.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.ServerID != srv.ID || got.FromUserID != srv.OwnerID || got.ToUserID != member {
		t.Errorf("got = %+v, want server=%v from=%v to=%v", got, srv.ID, srv.OwnerID, member)
	}
	if !got.IsPending() {
		t.Errorf("status = %v, want pending", got.Status)
	}
	if got.ResolvedAt != nil {
		t.Error("resolved_at should be nil for a pending transfer")
	}
}

func TestOwnershipTransferRepo_GetByID_NotFound(t *testing.T) {
	repo, _, _ := newRepo(t)

	_, err := repo.GetByID(context.Background(), uuid.New())
	if !errors.Is(err, model.ErrOwnershipTransferNotFound) {
		t.Errorf("err = %v, want ErrOwnershipTransferNotFound", err)
	}
}

func TestOwnershipTransferRepo_GetPendingByServer_ReturnsIt(t *testing.T) {
	repo, servers, pool := newRepo(t)
	ctx := context.Background()
	srv, member := seedServerWithMember(ctx, t, servers, pool)

	want := pendingTransfer(srv, member)
	if err := repo.Create(ctx, want); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.GetPendingByServer(ctx, srv.ID)
	if err != nil {
		t.Fatalf("GetPendingByServer: %v", err)
	}
	if got.ID != want.ID {
		t.Errorf("got id %v, want %v", got.ID, want.ID)
	}
}

func TestOwnershipTransferRepo_GetPendingByServer_None_NotFound(t *testing.T) {
	repo, servers, pool := newRepo(t)
	ctx := context.Background()
	srv, _ := seedServerWithMember(ctx, t, servers, pool)

	_, err := repo.GetPendingByServer(ctx, srv.ID)
	if !errors.Is(err, model.ErrOwnershipTransferNotFound) {
		t.Errorf("err = %v, want ErrOwnershipTransferNotFound", err)
	}
}

func TestOwnershipTransferRepo_GetPendingByServer_IgnoresResolved(t *testing.T) {
	repo, servers, pool := newRepo(t)
	ctx := context.Background()
	srv, member := seedServerWithMember(ctx, t, servers, pool)

	resolved := pendingTransfer(srv, member)
	if err := repo.Create(ctx, resolved); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := repo.Reject(ctx, resolved.ID, time.Now().UTC()); err != nil {
		t.Fatalf("Reject: %v", err)
	}

	_, err := repo.GetPendingByServer(ctx, srv.ID)
	if !errors.Is(err, model.ErrOwnershipTransferNotFound) {
		t.Errorf("err = %v, want ErrOwnershipTransferNotFound (resolved transfers are not pending)", err)
	}
}

func TestOwnershipTransferRepo_Create_SecondPending_AlreadyPending(t *testing.T) {
	repo, servers, pool := newRepo(t)
	ctx := context.Background()
	srv, member := seedServerWithMember(ctx, t, servers, pool)

	if err := repo.Create(ctx, pendingTransfer(srv, member)); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	err := repo.Create(ctx, pendingTransfer(srv, member))
	if !errors.Is(err, model.ErrOwnershipTransferAlreadyPending) {
		t.Errorf("err = %v, want ErrOwnershipTransferAlreadyPending", err)
	}
}

func TestOwnershipTransferRepo_Create_AfterResolved_AllowsNewPending(t *testing.T) {
	repo, servers, pool := newRepo(t)
	ctx := context.Background()
	srv, member := seedServerWithMember(ctx, t, servers, pool)

	first := pendingTransfer(srv, member)
	if err := repo.Create(ctx, first); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	if _, err := repo.Reject(ctx, first.ID, time.Now().UTC()); err != nil {
		t.Fatalf("Reject: %v", err)
	}

	if err := repo.Create(ctx, pendingTransfer(srv, member)); err != nil {
		t.Errorf("second Create after the first was resolved should succeed, got: %v", err)
	}
}

// CA3: rejecting leaves ownership completely untouched.
func TestOwnershipTransferRepo_Reject_LeavesOwnershipUnchanged(t *testing.T) {
	repo, servers, pool := newRepo(t)
	ctx := context.Background()
	srv, member := seedServerWithMember(ctx, t, servers, pool)
	transfer := pendingTransfer(srv, member)
	if err := repo.Create(ctx, transfer); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.Reject(ctx, transfer.ID, time.Now().UTC())
	if err != nil {
		t.Fatalf("Reject: %v", err)
	}
	if got.Status != model.OwnershipTransferRejected || got.ResolvedAt == nil {
		t.Errorf("got = %+v, want status=rejected with resolved_at set", got)
	}

	if serverOwnerID(ctx, t, pool, srv.ID) != srv.OwnerID {
		t.Error("servers.owner_id must not change on reject")
	}
	if !isOwnerFlag(ctx, t, pool, srv.ID, srv.OwnerID) {
		t.Error("original owner must still have is_owner=TRUE after reject")
	}
	if isOwnerFlag(ctx, t, pool, srv.ID, member) {
		t.Error("the target must not become owner after reject")
	}
}

func TestOwnershipTransferRepo_Reject_NotPending_ReturnsErr(t *testing.T) {
	repo, servers, pool := newRepo(t)
	ctx := context.Background()
	srv, member := seedServerWithMember(ctx, t, servers, pool)
	transfer := pendingTransfer(srv, member)
	if err := repo.Create(ctx, transfer); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := repo.Reject(ctx, transfer.ID, time.Now().UTC()); err != nil {
		t.Fatalf("first Reject: %v", err)
	}

	_, err := repo.Reject(ctx, transfer.ID, time.Now().UTC())
	if !errors.Is(err, model.ErrOwnershipTransferNotPending) {
		t.Errorf("err = %v, want ErrOwnershipTransferNotPending", err)
	}
}

// Cancelling withdraws a pending transfer without touching ownership, and
// frees the server up to start a new one right away.
func TestOwnershipTransferRepo_Cancel_LeavesOwnershipUnchanged(t *testing.T) {
	repo, servers, pool := newRepo(t)
	ctx := context.Background()
	srv, member := seedServerWithMember(ctx, t, servers, pool)
	transfer := pendingTransfer(srv, member)
	if err := repo.Create(ctx, transfer); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.Cancel(ctx, transfer.ID, time.Now().UTC())
	if err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if got.Status != model.OwnershipTransferCancelled || got.ResolvedAt == nil {
		t.Errorf("got = %+v, want status=cancelled with resolved_at set", got)
	}

	if serverOwnerID(ctx, t, pool, srv.ID) != srv.OwnerID {
		t.Error("servers.owner_id must not change on cancel")
	}
	if !isOwnerFlag(ctx, t, pool, srv.ID, srv.OwnerID) {
		t.Error("original owner must still have is_owner=TRUE after cancel")
	}
}

func TestOwnershipTransferRepo_Cancel_NotPending_ReturnsErr(t *testing.T) {
	repo, servers, pool := newRepo(t)
	ctx := context.Background()
	srv, member := seedServerWithMember(ctx, t, servers, pool)
	transfer := pendingTransfer(srv, member)
	if err := repo.Create(ctx, transfer); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := repo.Cancel(ctx, transfer.ID, time.Now().UTC()); err != nil {
		t.Fatalf("first Cancel: %v", err)
	}

	_, err := repo.Cancel(ctx, transfer.ID, time.Now().UTC())
	if !errors.Is(err, model.ErrOwnershipTransferNotPending) {
		t.Errorf("err = %v, want ErrOwnershipTransferNotPending", err)
	}
}

func TestOwnershipTransferRepo_Create_AfterCancelled_AllowsNewPending(t *testing.T) {
	repo, servers, pool := newRepo(t)
	ctx := context.Background()
	srv, member := seedServerWithMember(ctx, t, servers, pool)

	first := pendingTransfer(srv, member)
	if err := repo.Create(ctx, first); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	if _, err := repo.Cancel(ctx, first.ID, time.Now().UTC()); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	if err := repo.Create(ctx, pendingTransfer(srv, member)); err != nil {
		t.Errorf("second Create after the first was cancelled should succeed, got: %v", err)
	}
}

// CA1: accepting makes the recipient the new owner and demotes the previous one.
func TestOwnershipTransferRepo_Accept_TransfersOwnership(t *testing.T) {
	repo, servers, pool := newRepo(t)
	ctx := context.Background()
	srv, member := seedServerWithMember(ctx, t, servers, pool)
	transfer := pendingTransfer(srv, member)
	if err := repo.Create(ctx, transfer); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.Accept(ctx, transfer.ID, time.Now().UTC())
	if err != nil {
		t.Fatalf("Accept: %v", err)
	}
	if got.Status != model.OwnershipTransferAccepted || got.ResolvedAt == nil {
		t.Errorf("got = %+v, want status=accepted with resolved_at set", got)
	}

	if serverOwnerID(ctx, t, pool, srv.ID) != member {
		t.Error("servers.owner_id must be the recipient after accept")
	}
	if isOwnerFlag(ctx, t, pool, srv.ID, srv.OwnerID) {
		t.Error("the previous owner must be a regular member (is_owner=FALSE) after accept")
	}
	if !isOwnerFlag(ctx, t, pool, srv.ID, member) {
		t.Error("the recipient must have is_owner=TRUE after accept")
	}
}

func TestOwnershipTransferRepo_Accept_NotPending_ReturnsErr(t *testing.T) {
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

	_, err := repo.Accept(ctx, transfer.ID, time.Now().UTC())
	if !errors.Is(err, model.ErrOwnershipTransferNotPending) {
		t.Errorf("err = %v, want ErrOwnershipTransferNotPending", err)
	}
}

// If the recipient left the server after the transfer was created, accepting
// must fail and change nothing -- not the transfer's status, not ownership.
func TestOwnershipTransferRepo_Accept_TargetNoLongerMember_LeavesEverythingUnchanged(t *testing.T) {
	repo, servers, pool := newRepo(t)
	ctx := context.Background()
	srv, member := seedServerWithMember(ctx, t, servers, pool)
	transfer := pendingTransfer(srv, member)
	if err := repo.Create(ctx, transfer); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if _, err := pool.Exec(ctx,
		`DELETE FROM server_members WHERE server_id = $1 AND user_id = $2`, srv.ID, member,
	); err != nil {
		t.Fatalf("simulate the target leaving: %v", err)
	}

	_, err := repo.Accept(ctx, transfer.ID, time.Now().UTC())
	if !errors.Is(err, model.ErrTransferTargetNotMember) {
		t.Fatalf("err = %v, want ErrTransferTargetNotMember", err)
	}

	reloaded, err := repo.GetByID(ctx, transfer.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if !reloaded.IsPending() {
		t.Errorf("transfer status = %v, want still pending (accept must roll back)", reloaded.Status)
	}
	if serverOwnerID(ctx, t, pool, srv.ID) != srv.OwnerID {
		t.Error("servers.owner_id must not change when the target isn't a member anymore")
	}
}

func dropAllTestTables(ctx context.Context, pool *pgxpool.Pool) {
	_, _ = pool.Exec(ctx, `DROP TABLE IF EXISTS ownership_transfers, member_roles, roles, server_bans, invitations, server_members, channels, servers CASCADE`)
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
