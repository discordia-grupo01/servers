package member_repo

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/discordia-grupo01/servers/internal/model/role"
)

func (r *MemberRepository) FilterMembers(ctx context.Context, serverID uuid.UUID, userIDs []string) ([]string, error) {
	return r.queryUserIDs(ctx, "filter members", `
		SELECT user_id FROM server_members
		 WHERE server_id = $1 AND user_id = ANY($2)
		 ORDER BY user_id`,
		serverID, userIDs,
	)
}

func (r *MemberRepository) MemberIDsWithRoles(ctx context.Context, serverID uuid.UUID, roleIDs []uuid.UUID) ([]string, error) {
	return r.queryUserIDs(ctx, "members with roles", `
		SELECT DISTINCT user_id FROM member_roles
		 WHERE server_id = $1 AND role_id = ANY($2)
		 ORDER BY user_id`,
		serverID, roleIDs,
	)
}

func (r *MemberRepository) ViewersAmong(ctx context.Context, serverID uuid.UUID, ownerID string, userIDs []string) ([]string, error) {
	return r.queryUserIDs(ctx, "viewers among", viewersQuery,
		serverID, ownerID, viewMask, false, userIDs,
	)
}

func (r *MemberRepository) AllViewers(ctx context.Context, serverID uuid.UUID, ownerID string) ([]string, error) {
	return r.queryUserIDs(ctx, "all viewers", viewersQuery,
		serverID, ownerID, viewMask, true, []string{},
	)
}

// viewMask holds the bits that let a member see channels: VIEW_CHANNELS
// itself, or ADMINISTRATOR, which implies every permission.
const viewMask = role.PermViewChannels | role.PermAdministrator

const viewersQuery = `
	SELECT sm.user_id
	  FROM server_members sm
	  LEFT JOIN member_roles mr ON mr.server_id = sm.server_id AND mr.user_id = sm.user_id
	  LEFT JOIN roles r ON r.id = mr.role_id
	 WHERE sm.server_id = $1 AND ($4 OR sm.user_id = ANY($5))
	 GROUP BY sm.user_id
	HAVING sm.user_id = $2 OR (COALESCE(BIT_OR(r.permissions), 0) & $3) <> 0
	 ORDER BY sm.user_id`

func (r *MemberRepository) queryUserIDs(ctx context.Context, what, sql string, args ...any) ([]string, error) {
	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", what, err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("%s: %w", what, err)
	}
	return ids, nil
}
