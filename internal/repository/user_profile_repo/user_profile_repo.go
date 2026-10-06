// Package user_profile_repo implements Postgres access for the replica of
// users' public profiles (user_profiles).
package user_profile_repo

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/discordia-grupo01/servers/internal/model/userprofile"
)

type UserProfileRepository struct {
	pool *pgxpool.Pool
}

func NewUserProfileRepository(pool *pgxpool.Pool) *UserProfileRepository {
	return &UserProfileRepository{pool: pool}
}

func (r *UserProfileRepository) GetName(ctx context.Context, userID string) (string, error) {
	var name string
	err := r.pool.QueryRow(ctx, `SELECT name FROM user_profiles WHERE user_id = $1`, userID).Scan(&name)
	if err != nil {
		if err == pgx.ErrNoRows {
			return "", nil
		}
		return "", fmt.Errorf("get user profile name: %w", err)
	}
	return name, nil
}

// Upsert stores p unless a profile with the same or a higher version is
// already stored, and reports whether it was applied. Redelivered and
// out-of-order events are therefore harmless no-ops, in a single atomic
// statement.
func (r *UserProfileRepository) Upsert(ctx context.Context, p userprofile.Profile) (bool, error) {
	tag, err := r.pool.Exec(ctx, `
		INSERT INTO user_profiles (user_id, name, avatar_url, description, status_text, status_emoji, version, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, now())
		ON CONFLICT (user_id) DO UPDATE SET
			name         = EXCLUDED.name,
			avatar_url   = EXCLUDED.avatar_url,
			description  = EXCLUDED.description,
			status_text  = EXCLUDED.status_text,
			status_emoji = EXCLUDED.status_emoji,
			version      = EXCLUDED.version,
			updated_at   = now()
		WHERE user_profiles.version < EXCLUDED.version`,
		p.UserID, p.Name, p.AvatarURL, p.Description, p.StatusText, p.StatusEmoji, p.Version,
	)
	if err != nil {
		return false, fmt.Errorf("upsert user profile: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}
