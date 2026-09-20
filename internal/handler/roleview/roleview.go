// Package roleview holds the JSON representation of role.Role shared by
// role_handler and member_role_handler, so both endpoints serialize roles
// identically.
package roleview

import (
	"time"

	"github.com/discordia-grupo01/servers/internal/model/role"
)

type Response struct {
	ID          string    `json:"id"`
	ServerID    string    `json:"server_id"`
	Name        string    `json:"name"`
	Color       string    `json:"color"`
	Permissions []string  `json:"permissions"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func ToResponse(r role.Role) Response {
	return Response{
		ID:          r.ID.String(),
		ServerID:    r.ServerID.String(),
		Name:        r.Name,
		Color:       r.Color,
		Permissions: role.PermissionNames(r.Permissions),
		CreatedAt:   r.CreatedAt,
		UpdatedAt:   r.UpdatedAt,
	}
}
