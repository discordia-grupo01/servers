// Package categoryview holds the JSON representation of model.Category
// shared by category_handler and server_handler.
package categoryview

import "github.com/discordia-grupo01/servers/internal/model"

type Response struct {
	ID       string `json:"id"`
	ServerID string `json:"server_id"`
	Name     string `json:"name"`
	Position int    `json:"position"`
}

func ToResponse(cat model.Category) Response {
	return Response{
		ID:       cat.ID.String(),
		ServerID: cat.ServerID.String(),
		Name:     cat.Name,
		Position: cat.Position,
	}
}
