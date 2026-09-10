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

func newInviteRepo(t *testing.T) (*repository.InvitationRepository, *pgxpool.Pool) {
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

	_, _ = pool.Exec(ctx, `DROP TABLE IF EXISTS server_bans, invitations, server_members, channels, servers CASCADE`)
	for _, f := range []string{"0001_init.sql", "0002_invitations.sql"} {
		schema, err := os.ReadFile("../../migrations/" + f)
		if err != nil {
			t.Fatalf("read migration %s: %v", f, err)
		}
		if _, err := pool.Exec(ctx, string(schema)); err != nil {
			t.Fatalf("apply %s: %v", f, err)
		}
	}
	return repository.NewInvitationRepository(pool), pool
}

func seedServer(t *testing.T, pool *pgxpool.Pool, owner string) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	id := uuid.New()
	now := time.Now().UTC()
	if _, err := pool.Exec(ctx,
		`INSERT INTO servers (id, name, name_key, owner_id, created_at) VALUES ($1,$2,$3,$4,$5)`,
		id, "Srv "+id.String()[:8], id.String(), owner, now,
	); err != nil {
		t.Fatalf("seed server: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO server_members (server_id, user_id, is_owner, joined_at) VALUES ($1,$2,TRUE,$3)`,
		id, owner, now,
	); err != nil {
		t.Fatalf("seed owner membership: %v", err)
	}
	return id
}

func newInvitation(serverID uuid.UUID, creator string) model.Invitation {
	return model.Invitation{
		ID:        uuid.New(),
		Code:      "code-" + uuid.NewString(),
		ServerID:  serverID,
		CreatedBy: creator,
		CreatedAt: time.Now().UTC(),
	}
}

func TestInviteRepo_CreateAndFetchByCode(t *testing.T) {
	repo, pool := newInviteRepo(t)
	ctx := context.Background()
	owner := "owner-" + uuid.NewString()
	serverID := seedServer(t, pool, owner)

	inv := newInvitation(serverID, owner)
	inv.MaxUses = intPtr(5)
	exp := time.Now().UTC().Add(time.Hour).Truncate(time.Millisecond)
	inv.ExpiresAt = &exp

	if err := repo.CreateInvitation(ctx, inv); err != nil {
		t.Fatalf("CreateInvitation: %v", err)
	}

	got, err := repo.InvitationByCode(ctx, inv.Code)
	if err != nil {
		t.Fatalf("InvitationByCode: %v", err)
	}
	if got.ServerID != serverID || got.CreatedBy != owner || got.Uses != 0 {
		t.Errorf("got = %+v", got)
	}
	if got.MaxUses == nil || *got.MaxUses != 5 {
		t.Errorf("max_uses = %v, want 5", got.MaxUses)
	}
	if got.ExpiresAt == nil || !got.ExpiresAt.Equal(exp) {
		t.Errorf("expires_at = %v, want %v", got.ExpiresAt, exp)
	}
}

func TestInviteRepo_DuplicateCodeIsReported(t *testing.T) {
	repo, pool := newInviteRepo(t)
	ctx := context.Background()
	owner := "owner-" + uuid.NewString()
	serverID := seedServer(t, pool, owner)

	inv := newInvitation(serverID, owner)
	if err := repo.CreateInvitation(ctx, inv); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	dup := newInvitation(serverID, owner)
	dup.Code = inv.Code
	if err := repo.CreateInvitation(ctx, dup); !errors.Is(err, model.ErrInviteCodeUnavailable) {
		t.Fatalf("err = %v, want ErrInviteCodeUnavailable", err)
	}
}

func TestInviteRepo_UnknownCode(t *testing.T) {
	repo, _ := newInviteRepo(t)
	if _, err := repo.InvitationByCode(context.Background(), "does-not-exist"); !errors.Is(err, model.ErrInvitationNotFound) {
		t.Fatalf("err = %v, want ErrInvitationNotFound", err)
	}
}

func TestInviteRepo_MembershipAndBanChecks(t *testing.T) {
	repo, pool := newInviteRepo(t)
	ctx := context.Background()
	owner := "owner-" + uuid.NewString()
	serverID := seedServer(t, pool, owner)

	if ok, err := repo.IsMember(ctx, serverID, owner); err != nil || !ok {
		t.Fatalf("owner IsMember = %v, %v", ok, err)
	}
	if ok, _ := repo.IsMember(ctx, serverID, "stranger"); ok {
		t.Error("stranger reported as member")
	}
	if ok, _ := repo.IsBanned(ctx, serverID, "stranger"); ok {
		t.Error("stranger reported as banned before any ban")
	}

	if _, err := pool.Exec(ctx,
		`INSERT INTO server_bans (server_id, user_id) VALUES ($1,$2)`, serverID, "stranger",
	); err != nil {
		t.Fatalf("seed ban: %v", err)
	}
	if ok, err := repo.IsBanned(ctx, serverID, "stranger"); err != nil || !ok {
		t.Fatalf("IsBanned = %v, %v", ok, err)
	}
}

func TestInviteRepo_RedeemIncrementsUsesAndAddsMember(t *testing.T) {
	repo, pool := newInviteRepo(t)
	ctx := context.Background()
	owner := "owner-" + uuid.NewString()
	serverID := seedServer(t, pool, owner)

	inv := newInvitation(serverID, owner)
	inv.MaxUses = intPtr(1)
	if err := repo.CreateInvitation(ctx, inv); err != nil {
		t.Fatalf("CreateInvitation: %v", err)
	}

	gotServer, err := repo.RedeemInvitation(ctx, inv.Code, "newcomer", time.Now().UTC())
	if err != nil {
		t.Fatalf("RedeemInvitation: %v", err)
	}
	if gotServer != serverID {
		t.Errorf("server = %s, want %s", gotServer, serverID)
	}
	if ok, _ := repo.IsMember(ctx, serverID, "newcomer"); !ok {
		t.Error("newcomer was not added as a member")
	}

	after, _ := repo.InvitationByCode(ctx, inv.Code)
	if after.Uses != 1 {
		t.Errorf("uses = %d, want 1", after.Uses)
	}

	if _, err := repo.RedeemInvitation(ctx, inv.Code, "another", time.Now().UTC()); !errors.Is(err, model.ErrInvitationExhausted) {
		t.Fatalf("err = %v, want ErrInvitationExhausted", err)
	}
}

func TestInviteRepo_RedeemForExistingMemberDoesNotConsumeUse(t *testing.T) {
	repo, pool := newInviteRepo(t)
	ctx := context.Background()
	owner := "owner-" + uuid.NewString()
	serverID := seedServer(t, pool, owner)

	inv := newInvitation(serverID, owner)
	inv.MaxUses = intPtr(3)
	if err := repo.CreateInvitation(ctx, inv); err != nil {
		t.Fatalf("CreateInvitation: %v", err)
	}

	if _, err := repo.RedeemInvitation(ctx, inv.Code, owner, time.Now().UTC()); !errors.Is(err, model.ErrAlreadyMember) {
		t.Fatalf("err = %v, want ErrAlreadyMember", err)
	}
	after, _ := repo.InvitationByCode(ctx, inv.Code)
	if after.Uses != 0 {
		t.Errorf("uses = %d, want 0 (rolled back)", after.Uses)
	}
}

func TestInviteRepo_RedeemExpiredInvitation(t *testing.T) {
	repo, pool := newInviteRepo(t)
	ctx := context.Background()
	owner := "owner-" + uuid.NewString()
	serverID := seedServer(t, pool, owner)

	inv := newInvitation(serverID, owner)
	past := time.Now().UTC().Add(-time.Hour)
	inv.ExpiresAt = &past
	if err := repo.CreateInvitation(ctx, inv); err != nil {
		t.Fatalf("CreateInvitation: %v", err)
	}

	if _, err := repo.RedeemInvitation(ctx, inv.Code, "late", time.Now().UTC()); !errors.Is(err, model.ErrInvitationExhausted) {
		t.Fatalf("err = %v, want ErrInvitationExhausted", err)
	}
}

func TestInviteRepo_Revoke(t *testing.T) {
	repo, pool := newInviteRepo(t)
	ctx := context.Background()
	owner := "owner-" + uuid.NewString()
	serverID := seedServer(t, pool, owner)

	inv := newInvitation(serverID, owner)
	if err := repo.CreateInvitation(ctx, inv); err != nil {
		t.Fatalf("CreateInvitation: %v", err)
	}
	if err := repo.RevokeInvitation(ctx, inv.Code, time.Now().UTC()); err != nil {
		t.Fatalf("RevokeInvitation: %v", err)
	}

	got, _ := repo.InvitationByCode(ctx, inv.Code)
	if !got.IsRevoked() {
		t.Error("invitation is not marked as revoked")
	}
	if _, err := repo.RedeemInvitation(ctx, inv.Code, "nope", time.Now().UTC()); !errors.Is(err, model.ErrInvitationExhausted) {
		t.Fatalf("err = %v, want ErrInvitationExhausted after revoke", err)
	}
}

func intPtr(n int) *int { return &n }
