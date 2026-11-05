// Package boost_repo keeps servers' side of the boost saga. Every step locks the
// server row, so two boosts of a server can't count at the same time and the
// counter, the answer to billing and its event commit together.
package boost_repo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/discordia-grupo01/events-contract/boosts"
	"github.com/discordia-grupo01/events-contract/envelope"
	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/outbox"
)

const (
	pgUniqueViolation = "23505"
	uqVanityCode      = "servers_vanity_code_key"
)

type Repository struct {
	pool   *pgxpool.Pool
	secret string
}

func NewRepository(pool *pgxpool.Pool, secret string) *Repository {
	return &Repository{pool: pool, secret: secret}
}

// Request answers billing.server_boost_requested. A boost already seen gets the
// same answer as the first time, because billing republishes what it has pending.
// A missing server leaves no row, but it can't come back, so the answer repeats.
func (r *Repository) Request(ctx context.Context, b model.Boost) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		var suspendedAt *time.Time
		err := tx.QueryRow(ctx,
			`SELECT suspended_at FROM servers WHERE id = $1 FOR NO KEY UPDATE`, b.ServerID,
		).Scan(&suspendedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return r.insertSaga(ctx, tx, boosts.RoutingKeyRejected, b, boosts.ReasonServerNotFound)
		}
		if err != nil {
			return fmt.Errorf("lock server: %w", err)
		}

		var status string
		var reason *string
		err = tx.QueryRow(ctx,
			`SELECT status, reason FROM server_boosts WHERE boost_id = $1`, b.ID,
		).Scan(&status, &reason)
		switch {
		case err == nil:
			return r.repeatAnswer(ctx, tx, b, status, reason)
		case !errors.Is(err, pgx.ErrNoRows):
			return fmt.Errorf("select boost: %w", err)
		}

		rejection, err := rejectionReason(ctx, tx, b, suspendedAt)
		if err != nil {
			return err
		}
		if rejection != "" {
			if err := insertBoost(ctx, tx, b, model.BoostStatusRejected, &rejection); err != nil {
				return err
			}
			return r.insertSaga(ctx, tx, boosts.RoutingKeyRejected, b, rejection)
		}

		if err := insertBoost(ctx, tx, b, model.BoostStatusActive, nil); err != nil {
			return err
		}
		if err := r.insertSaga(ctx, tx, boosts.RoutingKeyApplied, b, ""); err != nil {
			return err
		}
		return r.recount(ctx, tx, b.ServerID)
	})
}

// Cancel answers billing.server_boost_cancelled: billing already refunded the
// points. If the request hasn't arrived, the row stays so it is rejected when it
// does; if the boost was counted, it stops counting.
func (r *Repository) Cancel(ctx context.Context, b model.Boost) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		found, err := lockServer(ctx, tx, b.ServerID)
		if err != nil || !found {
			return err
		}
		tag, err := tx.Exec(ctx, `
			INSERT INTO server_boosts (boost_id, server_id, user_id, status)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (boost_id) DO NOTHING`,
			b.ID, b.ServerID, b.UserID, model.BoostStatusCancelled)
		if err != nil {
			return fmt.Errorf("insert cancelled boost: %w", err)
		}
		if tag.RowsAffected() == 1 {
			return nil
		}
		return r.deactivate(ctx, tx, b, model.BoostStatusCancelled)
	})
}

// Remove answers billing.server_boost_removed. billing only removes active
// boosts, so a missing row means the server is gone and there is nothing to do.
func (r *Repository) Remove(ctx context.Context, b model.Boost) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		found, err := lockServer(ctx, tx, b.ServerID)
		if err != nil || !found {
			return err
		}
		return r.deactivate(ctx, tx, b, model.BoostStatusRemoved)
	})
}

// SetVanity sets the code or, with nil, clears it. A code can't match an
// invitation's, because joining looks the invitation up first and would never
// reach this server.
func (r *Repository) SetVanity(ctx context.Context, serverID uuid.UUID, code *string) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		if code != nil {
			var used bool
			if err := tx.QueryRow(ctx,
				`SELECT EXISTS (SELECT 1 FROM invitations WHERE code = $1)`, *code,
			).Scan(&used); err != nil {
				return fmt.Errorf("check invitation code: %w", err)
			}
			if used {
				return model.ErrVanityCodeTaken
			}
		}
		tag, err := tx.Exec(ctx, `UPDATE servers SET vanity_code = $1 WHERE id = $2`, code, serverID)
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation && pgErr.ConstraintName == uqVanityCode {
				return model.ErrVanityCodeTaken
			}
			return fmt.Errorf("update vanity code: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return model.ErrServerNotFound
		}
		return nil
	})
}

func (r *Repository) repeatAnswer(ctx context.Context, tx pgx.Tx, b model.Boost, status string, reason *string) error {
	switch status {
	case model.BoostStatusActive:
		return r.insertSaga(ctx, tx, boosts.RoutingKeyApplied, b, "")
	case model.BoostStatusRejected:
		return r.insertSaga(ctx, tx, boosts.RoutingKeyRejected, b, *reason)
	case model.BoostStatusCancelled:
		return r.insertSaga(ctx, tx, boosts.RoutingKeyRejected, b, boosts.ReasonCancelled)
	default:
		return nil
	}
}

func rejectionReason(ctx context.Context, tx pgx.Tx, b model.Boost, suspendedAt *time.Time) (string, error) {
	if suspendedAt != nil {
		return boosts.ReasonServerSuspended, nil
	}
	var member bool
	if err := tx.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM server_members WHERE server_id = $1 AND user_id = $2)`,
		b.ServerID, b.UserID,
	).Scan(&member); err != nil {
		return "", fmt.Errorf("check membership: %w", err)
	}
	if !member {
		return boosts.ReasonNotAMember, nil
	}
	return "", nil
}

func lockServer(ctx context.Context, tx pgx.Tx, serverID uuid.UUID) (bool, error) {
	var locked int
	err := tx.QueryRow(ctx, `SELECT 1 FROM servers WHERE id = $1 FOR NO KEY UPDATE`, serverID).Scan(&locked)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("lock server: %w", err)
	}
	return true, nil
}

func insertBoost(ctx context.Context, tx pgx.Tx, b model.Boost, status string, reason *string) error {
	if _, err := tx.Exec(ctx, `
		INSERT INTO server_boosts (boost_id, server_id, user_id, status, reason)
		VALUES ($1, $2, $3, $4, $5)`,
		b.ID, b.ServerID, b.UserID, status, reason,
	); err != nil {
		return fmt.Errorf("insert boost: %w", err)
	}
	return nil
}

func (r *Repository) deactivate(ctx context.Context, tx pgx.Tx, b model.Boost, status string) error {
	tag, err := tx.Exec(ctx, `
		UPDATE server_boosts SET status = $2, updated_at = now()
		 WHERE boost_id = $1 AND status = $3`,
		b.ID, status, model.BoostStatusActive)
	if err != nil {
		return fmt.Errorf("deactivate boost: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil
	}
	return r.recount(ctx, tx, b.ServerID)
}

func (r *Repository) recount(ctx context.Context, tx pgx.Tx, serverID uuid.UUID) error {
	var count int
	if err := tx.QueryRow(ctx,
		`SELECT COUNT(*) FROM server_boosts WHERE server_id = $1 AND status = $2`,
		serverID, model.BoostStatusActive,
	).Scan(&count); err != nil {
		return fmt.Errorf("count boosts: %w", err)
	}
	level := model.BoostLevelFor(count)
	var version int64
	if err := tx.QueryRow(ctx, `
		UPDATE servers SET boost_count = $2, boost_level = $3, boost_version = boost_version + 1
		 WHERE id = $1
		RETURNING boost_version`,
		serverID, count, level,
	).Scan(&version); err != nil {
		return fmt.Errorf("update boost count: %w", err)
	}
	evt, err := outbox.New(boosts.RoutingKeyUpdated, boosts.ServerBoostUpdated{
		Meta:       newMeta(),
		ServerID:   serverID.String(),
		BoostCount: count,
		Level:      level,
		Version:    version,
	})
	if err != nil {
		return err
	}
	return outbox.Insert(ctx, tx, evt)
}

// insertSaga signs when the event is written, so the outbox poller publishes it
// as it does any other event.
func (r *Repository) insertSaga(ctx context.Context, tx pgx.Tx, routingKey string, b model.Boost, reason string) error {
	evt := boosts.SagaEvent{
		Meta:     newMeta(),
		BoostID:  b.ID.String(),
		ServerID: b.ServerID.String(),
		UserID:   b.UserID,
		Reason:   reason,
	}
	evt.Sign(routingKey, r.secret)
	row, err := outbox.New(routingKey, evt)
	if err != nil {
		return err
	}
	return outbox.Insert(ctx, tx, row)
}

func newMeta() envelope.Meta {
	return envelope.Meta{EventID: uuid.NewString(), OccurredAt: time.Now().UTC()}
}
