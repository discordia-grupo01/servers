package suspension_repo_test

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/discordia-grupo01/servers/internal/repository"
	"github.com/discordia-grupo01/servers/internal/repository/suspension_repo"
)

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func newRepo(t *testing.T) (*suspension_repo.SuspensionRepository, *pgxpool.Pool) {
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
	drop := func() { _, _ = pool.Exec(ctx, `DROP TABLE IF EXISTS suspended_users`) }
	t.Cleanup(drop)
	drop()

	schema, err := os.ReadFile("../../../migrations/0027_suspended_users.up.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	if _, err := pool.Exec(ctx, string(schema)); err != nil {
		t.Fatalf("apply migration: %v", err)
	}
	return suspension_repo.NewSuspensionRepository(pool), pool
}

func mustApply(t *testing.T, repo *suspension_repo.SuspensionRepository, user string, suspended bool, version int64, want bool) {
	t.Helper()
	got, err := repo.Apply(context.Background(), user, suspended, version)
	if err != nil {
		t.Fatalf("Apply(%s, %v, %d): %v", user, suspended, version, err)
	}
	if got != want {
		t.Errorf("Apply(%s, %v, %d) applied = %v, want %v", user, suspended, version, got, want)
	}
}

func mustBeSuspended(t *testing.T, repo *suspension_repo.SuspensionRepository, user string, want bool) {
	t.Helper()
	got, err := repo.IsSuspended(context.Background(), user)
	if err != nil {
		t.Fatalf("IsSuspended(%s): %v", user, err)
	}
	if got != want {
		t.Errorf("IsSuspended(%s) = %v, want %v", user, got, want)
	}
}

func TestIsSuspended_UnknownUserIsNotSuspended(t *testing.T) {
	repo, _ := newRepo(t)
	mustBeSuspended(t, repo, uuid.NewString(), false)
}

// Admin can suspend someone servers has never seen: the row is inserted anyway.
func TestApply_FirstEventForUnknownUserIsStored(t *testing.T) {
	repo, _ := newRepo(t)
	user := uuid.NewString()

	mustApply(t, repo, user, true, 1, true)

	mustBeSuspended(t, repo, user, true)
}

func TestApply_ReactivationWithHigherVersionLiftsTheSuspension(t *testing.T) {
	repo, pool := newRepo(t)
	user := uuid.NewString()
	mustApply(t, repo, user, true, 1, true)

	mustApply(t, repo, user, false, 2, true)

	mustBeSuspended(t, repo, user, false)
	var version int64
	if err := pool.QueryRow(context.Background(), `SELECT version FROM suspended_users WHERE user_id = $1`, user).Scan(&version); err != nil || version != 2 {
		t.Errorf("stored version = %d (err %v), want 2: the row is kept to remember it", version, err)
	}
}

func TestApply_SameOrLowerVersionIsANoOp(t *testing.T) {
	repo, _ := newRepo(t)
	user := uuid.NewString()
	mustApply(t, repo, user, true, 5, true)

	mustApply(t, repo, user, true, 5, false)  // duplicate
	mustApply(t, repo, user, false, 5, false) // same version, other state
	mustApply(t, repo, user, false, 4, false) // older

	mustBeSuspended(t, repo, user, true)
}

func TestApply_StaleSuspensionDoesNotUndoANewerReactivation(t *testing.T) {
	repo, _ := newRepo(t)
	user := uuid.NewString()
	mustApply(t, repo, user, false, 4, true)

	mustApply(t, repo, user, true, 3, false)

	mustBeSuspended(t, repo, user, false)
}

func TestApply_UsersAreIndependent(t *testing.T) {
	repo, _ := newRepo(t)
	a, b := uuid.NewString(), uuid.NewString()

	mustApply(t, repo, a, true, 1, true)

	mustBeSuspended(t, repo, a, true)
	mustBeSuspended(t, repo, b, false)
}
