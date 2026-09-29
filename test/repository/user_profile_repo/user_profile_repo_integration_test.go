package user_profile_repo_test

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/discordia-grupo01/servers/internal/model/userprofile"
	"github.com/discordia-grupo01/servers/internal/repository"
	"github.com/discordia-grupo01/servers/internal/repository/user_profile_repo"
)

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func newRepo(t *testing.T) (*user_profile_repo.UserProfileRepository, *pgxpool.Pool) {
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
	drop := func() { _, _ = pool.Exec(ctx, `DROP TABLE IF EXISTS user_profiles`) }
	t.Cleanup(drop)
	drop()

	schema, err := os.ReadFile("../../../migrations/0017_user_profiles.up.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	if _, err := pool.Exec(ctx, string(schema)); err != nil {
		t.Fatalf("apply migration: %v", err)
	}
	return user_profile_repo.NewUserProfileRepository(pool), pool
}

func profile(userID string, version int64, name string) userprofile.Profile {
	return userprofile.Profile{
		UserID: userID, Name: name, AvatarURL: "https://cdn/" + name + ".png",
		Description: "about " + name, StatusText: "playing", StatusEmoji: "🎮", Version: version,
	}
}

func stored(t *testing.T, pool *pgxpool.Pool, userID string) userprofile.Profile {
	t.Helper()
	var p userprofile.Profile
	err := pool.QueryRow(context.Background(), `
		SELECT user_id, name, avatar_url, description, status_text, status_emoji, version
		  FROM user_profiles WHERE user_id = $1`, userID,
	).Scan(&p.UserID, &p.Name, &p.AvatarURL, &p.Description, &p.StatusText, &p.StatusEmoji, &p.Version)
	if err != nil {
		t.Fatalf("read stored profile: %v", err)
	}
	return p
}

func TestUpsert_InsertsWhenMissing(t *testing.T) {
	repo, pool := newRepo(t)
	want := profile(uuid.NewString(), 1, "manu")

	applied, err := repo.Upsert(context.Background(), want)
	if err != nil || !applied {
		t.Fatalf("Upsert() = %v, %v; want true, nil", applied, err)
	}
	if got := stored(t, pool, want.UserID); got != want {
		t.Errorf("stored = %+v, want %+v", got, want)
	}
}

func TestUpsert_AppliesHigherVersion(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	userID := uuid.NewString()
	if _, err := repo.Upsert(ctx, profile(userID, 1, "old")); err != nil {
		t.Fatalf("seed: %v", err)
	}

	want := profile(userID, 2, "new")
	applied, err := repo.Upsert(ctx, want)
	if err != nil || !applied {
		t.Fatalf("Upsert(v2) = %v, %v; want true, nil", applied, err)
	}
	if got := stored(t, pool, userID); got != want {
		t.Errorf("stored = %+v, want %+v", got, want)
	}
}

// A redelivered event (same version) is a no-op.
func TestUpsert_IgnoresSameVersion(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	userID := uuid.NewString()
	want := profile(userID, 3, "first")
	if _, err := repo.Upsert(ctx, want); err != nil {
		t.Fatalf("seed: %v", err)
	}

	applied, err := repo.Upsert(ctx, profile(userID, 3, "duplicate"))
	if err != nil || applied {
		t.Fatalf("Upsert(same version) = %v, %v; want false, nil", applied, err)
	}
	if got := stored(t, pool, userID); got != want {
		t.Errorf("stored = %+v, want %+v", got, want)
	}
}

// An event that arrives out of order (lower version) doesn't overwrite a
// newer profile.
func TestUpsert_IgnoresLowerVersion(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	userID := uuid.NewString()
	want := profile(userID, 3, "v3")
	if _, err := repo.Upsert(ctx, want); err != nil {
		t.Fatalf("seed: %v", err)
	}

	applied, err := repo.Upsert(ctx, profile(userID, 2, "v2"))
	if err != nil || applied {
		t.Fatalf("Upsert(lower version) = %v, %v; want false, nil", applied, err)
	}
	if got := stored(t, pool, userID); got != want {
		t.Errorf("stored = %+v, want %+v", got, want)
	}
}
