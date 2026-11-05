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
	t.Cleanup(func() { dropAllTestTables(ctx, pool) })

	dropAllTestTables(ctx, pool)
	for _, f := range []string{
		"0001_init.up.sql", "0002_invitations.up.sql",
		"0003_roles.up.sql", "0004_member_roles.up.sql", "0005_default_role.up.sql",
		"0011_outbox_events.up.sql", "0015_role_positions.up.sql",
		"0019_everyone_role.up.sql",
		"0031_server_boosts.up.sql",
	} {
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

func TestInviteRepo_ListByServer_ReturnsAllRegardlessOfStatus(t *testing.T) {
	repo, pool := newInviteRepo(t)
	ctx := context.Background()
	owner := "owner-" + uuid.NewString()
	serverID := seedServer(t, pool, owner)

	active := newInvitation(serverID, owner)
	if err := repo.CreateInvitation(ctx, active); err != nil {
		t.Fatalf("create active: %v", err)
	}
	revoked := newInvitation(serverID, owner)
	if err := repo.CreateInvitation(ctx, revoked); err != nil {
		t.Fatalf("create revoked: %v", err)
	}
	if err := repo.RevokeInvitation(ctx, revoked.Code, time.Now().UTC()); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	got, err := repo.ListByServer(ctx, serverID)
	if err != nil {
		t.Fatalf("ListByServer: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d invitations, want 2", len(got))
	}
	codes := map[string]bool{got[0].Code: true, got[1].Code: true}
	if !codes[active.Code] || !codes[revoked.Code] {
		t.Errorf("codes = %v, want both %q and %q (revoked included)", codes, active.Code, revoked.Code)
	}
}

func TestInviteRepo_ListByServer_MostRecentFirst(t *testing.T) {
	repo, pool := newInviteRepo(t)
	ctx := context.Background()
	owner := "owner-" + uuid.NewString()
	serverID := seedServer(t, pool, owner)

	older := newInvitation(serverID, owner)
	older.CreatedAt = time.Now().UTC().Add(-time.Hour)
	if err := repo.CreateInvitation(ctx, older); err != nil {
		t.Fatalf("create older: %v", err)
	}
	newer := newInvitation(serverID, owner)
	newer.CreatedAt = time.Now().UTC()
	if err := repo.CreateInvitation(ctx, newer); err != nil {
		t.Fatalf("create newer: %v", err)
	}

	got, err := repo.ListByServer(ctx, serverID)
	if err != nil {
		t.Fatalf("ListByServer: %v", err)
	}
	if len(got) != 2 || got[0].Code != newer.Code || got[1].Code != older.Code {
		t.Errorf("got = %+v, want [newer, older]", got)
	}
}

func TestInviteRepo_ListByServer_OnlyThatServersInvitations(t *testing.T) {
	repo, pool := newInviteRepo(t)
	ctx := context.Background()

	mine := seedServer(t, pool, "owner-"+uuid.NewString())
	other := seedServer(t, pool, "owner-"+uuid.NewString())

	inv := newInvitation(mine, "owner-mine")
	if err := repo.CreateInvitation(ctx, inv); err != nil {
		t.Fatalf("create: %v", err)
	}
	otherInv := newInvitation(other, "owner-other")
	if err := repo.CreateInvitation(ctx, otherInv); err != nil {
		t.Fatalf("create other: %v", err)
	}

	got, err := repo.ListByServer(ctx, mine)
	if err != nil {
		t.Fatalf("ListByServer: %v", err)
	}
	if len(got) != 1 || got[0].Code != inv.Code {
		t.Errorf("got = %+v, want only %q", got, inv.Code)
	}
}

func TestInviteRepo_ListByServer_NoInvitations_ReturnsEmptyNotNil(t *testing.T) {
	repo, pool := newInviteRepo(t)
	serverID := seedServer(t, pool, "owner-"+uuid.NewString())

	got, err := repo.ListByServer(context.Background(), serverID)
	if err != nil {
		t.Fatalf("ListByServer: %v", err)
	}
	if got == nil {
		t.Error("got nil, want an empty (non-nil) slice")
	}
	if len(got) != 0 {
		t.Errorf("got %d invitations, want 0", len(got))
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

	gotServer, err := repo.RedeemInvitation(ctx, inv.Code, "newcomer", time.Now().UTC(), testBuildEvent)
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

	if _, err := repo.RedeemInvitation(ctx, inv.Code, "another", time.Now().UTC(), testBuildEvent); !errors.Is(err, model.ErrInvitationExhausted) {
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

	if _, err := repo.RedeemInvitation(ctx, inv.Code, owner, time.Now().UTC(), testBuildEvent); !errors.Is(err, model.ErrAlreadyMember) {
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

	if _, err := repo.RedeemInvitation(ctx, inv.Code, "late", time.Now().UTC(), testBuildEvent); !errors.Is(err, model.ErrInvitationExhausted) {
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
	if _, err := repo.RedeemInvitation(ctx, inv.Code, "nope", time.Now().UTC(), testBuildEvent); !errors.Is(err, model.ErrInvitationExhausted) {
		t.Fatalf("err = %v, want ErrInvitationExhausted after revoke", err)
	}
}

func TestInviteRepo_RedeemAssignsDefaultRole(t *testing.T) {
	repo, pool := newInviteRepo(t)
	ctx := context.Background()
	owner := "owner-" + uuid.NewString()
	serverID := seedServer(t, pool, owner)

	roleID := uuid.New()
	if _, err := pool.Exec(ctx,
		`INSERT INTO roles (id, server_id, name, color, permissions, position) VALUES ($1,$2,'member','#112233',0,1)`,
		roleID, serverID,
	); err != nil {
		t.Fatalf("seed role: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`UPDATE servers SET default_role_id = $1 WHERE id = $2`, roleID, serverID,
	); err != nil {
		t.Fatalf("set default role: %v", err)
	}

	inv := newInvitation(serverID, owner)
	if err := repo.CreateInvitation(ctx, inv); err != nil {
		t.Fatalf("CreateInvitation: %v", err)
	}

	if _, err := repo.RedeemInvitation(ctx, inv.Code, "newcomer", time.Now().UTC(), testBuildEvent); err != nil {
		t.Fatalf("RedeemInvitation: %v", err)
	}

	var hasRole bool
	if err := pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM member_roles WHERE server_id = $1 AND user_id = $2 AND role_id = $3)`,
		serverID, "newcomer", roleID,
	).Scan(&hasRole); err != nil {
		t.Fatalf("check member_roles: %v", err)
	}
	if !hasRole {
		t.Error("newcomer was not assigned the server's default role")
	}
}

func TestInviteRepo_RedeemWithoutDefaultRoleAssignsNothing(t *testing.T) {
	repo, pool := newInviteRepo(t)
	ctx := context.Background()
	owner := "owner-" + uuid.NewString()
	serverID := seedServer(t, pool, owner)

	inv := newInvitation(serverID, owner)
	if err := repo.CreateInvitation(ctx, inv); err != nil {
		t.Fatalf("CreateInvitation: %v", err)
	}

	if _, err := repo.RedeemInvitation(ctx, inv.Code, "newcomer", time.Now().UTC(), testBuildEvent); err != nil {
		t.Fatalf("RedeemInvitation: %v", err)
	}

	var count int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM member_roles WHERE server_id = $1 AND user_id = $2`,
		serverID, "newcomer",
	).Scan(&count); err != nil {
		t.Fatalf("count member_roles: %v", err)
	}
	if count != 0 {
		t.Errorf("member_roles count = %d, want 0 (no default role configured)", count)
	}
}

// The redeem transaction re-checks the ban itself: a banned user is rejected
// without consuming a use nor becoming a member.
func TestInviteRepo_RedeemBannedUser_IsRejected(t *testing.T) {
	repo, pool := newInviteRepo(t)
	ctx := context.Background()
	owner := "owner-" + uuid.NewString()
	serverID := seedServer(t, pool, owner)
	inv := newInvitation(serverID, owner)
	if err := repo.CreateInvitation(ctx, inv); err != nil {
		t.Fatalf("CreateInvitation: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO server_bans (server_id, user_id) VALUES ($1, 'banned')`, serverID); err != nil {
		t.Fatal(err)
	}

	if _, err := repo.RedeemInvitation(ctx, inv.Code, "banned", time.Now().UTC(), testBuildEvent); !errors.Is(err, model.ErrUserBanned) {
		t.Fatalf("err = %v, want ErrUserBanned", err)
	}
	if ok, _ := repo.IsMember(ctx, serverID, "banned"); ok {
		t.Error("the banned user became a member")
	}
	if after, _ := repo.InvitationByCode(ctx, inv.Code); after.Uses != 0 {
		t.Errorf("uses = %d, want 0 (the rejected join must not consume a use)", after.Uses)
	}
}

// #25 CA1 under concurrency: a join racing a ban can't slip in. While the ban
// holds the server lock (as moderation_repo.BanMember does), the join waits,
// and once the ban commits the join sees it and is rejected.
func TestInviteRepo_RedeemWaitsForAConcurrentBan(t *testing.T) {
	repo, pool := newInviteRepo(t)
	ctx := context.Background()
	owner := "owner-" + uuid.NewString()
	serverID := seedServer(t, pool, owner)
	inv := newInvitation(serverID, owner)
	if err := repo.CreateInvitation(ctx, inv); err != nil {
		t.Fatalf("CreateInvitation: %v", err)
	}

	ban, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ban.Rollback(ctx) }()
	if _, err := ban.Exec(ctx, `SELECT 1 FROM servers WHERE id = $1 FOR NO KEY UPDATE`, serverID); err != nil {
		t.Fatal(err)
	}
	if _, err := ban.Exec(ctx, `INSERT INTO server_bans (server_id, user_id) VALUES ($1, 'racer')`, serverID); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := repo.RedeemInvitation(ctx, inv.Code, "racer", time.Now().UTC(), testBuildEvent)
		done <- err
	}()
	select {
	case err := <-done:
		t.Fatalf("the join finished while the ban held the server lock: err = %v", err)
	case <-time.After(300 * time.Millisecond):
	}

	if err := ban.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, model.ErrUserBanned) {
			t.Fatalf("err = %v, want ErrUserBanned", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the join never finished")
	}
	if ok, _ := repo.IsMember(ctx, serverID, "racer"); ok {
		t.Error("the banned user became a member")
	}
}

func intPtr(n int) *int { return &n }

func setVanity(t *testing.T, pool *pgxpool.Pool, serverID uuid.UUID, code string, level int) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		`UPDATE servers SET vanity_code = $1, boost_level = $2 WHERE id = $3`, code, level, serverID,
	); err != nil {
		t.Fatal(err)
	}
}

func TestInviteRepo_ServerByVanity_OnlyWhileTheLevelUnlocksIt(t *testing.T) {
	repo, pool := newInviteRepo(t)
	ctx := context.Background()
	serverID := seedServer(t, pool, "owner-1")

	setVanity(t, pool, serverID, "mi-servidor", model.BoostLevelVanity)
	got, err := repo.ServerByVanity(ctx, "mi-servidor")
	if err != nil || got != serverID {
		t.Errorf("got %s err=%v", got, err)
	}

	setVanity(t, pool, serverID, "mi-servidor", model.BoostLevelVanity-1)
	if _, err := repo.ServerByVanity(ctx, "mi-servidor"); !errors.Is(err, model.ErrInvitationNotFound) {
		t.Errorf("below the level: err = %v", err)
	}
}

func TestInviteRepo_JoinByVanity_AddsMemberAndEvent(t *testing.T) {
	repo, pool := newInviteRepo(t)
	ctx := context.Background()
	serverID := seedServer(t, pool, "owner-1")
	build := func(id uuid.UUID) (outbox.Event, error) {
		return outbox.Event{RoutingKey: "servers.member_joined", Payload: []byte(`{}`)}, nil
	}

	if err := repo.JoinByVanity(ctx, serverID, "new-user", time.Now(), build); err != nil {
		t.Fatal(err)
	}
	if member, err := repo.IsMember(ctx, serverID, "new-user"); err != nil || !member {
		t.Errorf("member=%v err=%v", member, err)
	}
	var events int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM outbox_events WHERE routing_key = 'servers.member_joined'`).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if events != 1 {
		t.Errorf("%d member_joined events, want 1", events)
	}

	if err := repo.JoinByVanity(ctx, serverID, "new-user", time.Now(), build); !errors.Is(err, model.ErrAlreadyMember) {
		t.Errorf("second join: err = %v", err)
	}
}
