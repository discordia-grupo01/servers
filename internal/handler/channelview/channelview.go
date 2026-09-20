// Package channelview holds the JSON representation of model.Channel shared
// by channel_handler and server_handler, so both endpoints serialize
// channels identically.
package channelview

import "github.com/discordia-grupo01/servers/internal/model"

type Response struct {
	ID         string  `json:"id"`
	ServerID   string  `json:"server_id"`
	Name       string  `json:"name"`
	Kind       string  `json:"kind"`
	Position   int     `json:"position"`
	CategoryID *string `json:"category_id"`
	Topic      *string `json:"topic"`
}

func ToResponse(ch model.Channel) Response {
	var categoryID *string
	if ch.CategoryID != nil {
		s := ch.CategoryID.String()
		categoryID = &s
	}
	return Response{
		ID:         ch.ID.String(),
		ServerID:   ch.ServerID.String(),
		Name:       ch.Name,
		Kind:       string(ch.Kind),
		Position:   ch.Position,
		CategoryID: categoryID,
		Topic:      ch.Topic,
	}
}
