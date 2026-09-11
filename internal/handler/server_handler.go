package handler

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/discordia-grupo01/servers/internal/apierr"
	"github.com/discordia-grupo01/servers/internal/middleware"
	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/service"
)

const (
	maxIconUploadBytes = 20 << 20
	dbPingTimeout      = 2 * time.Second
)

type ServerUseCases interface {
	CreateServer(ctx context.Context, p service.CreateServerParams) (model.Server, error)
}

type serverHandler struct {
	uc ServerUseCases
}

type channelResponse struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	Position int    `json:"position"`
}

type serverResponse struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	IconURL   *string           `json:"icon_url"`
	OwnerID   string            `json:"owner_id"`
	CreatedAt time.Time         `json:"created_at"`
	Channels  []channelResponse `json:"channels"`
}

func toServerResponse(s model.Server) serverResponse {
	chs := make([]channelResponse, 0, len(s.Channels))
	for _, ch := range s.Channels {
		chs = append(chs, channelResponse{
			ID: ch.ID.String(), Name: ch.Name, Kind: string(ch.Kind), Position: ch.Position,
		})
	}
	return serverResponse{
		ID: s.ID.String(), Name: s.Name, IconURL: s.IconURL, OwnerID: s.OwnerID,
		CreatedAt: s.CreatedAt, Channels: chs,
	}
}

// POST /v1/servers
func (h *serverHandler) createServer(c *gin.Context) {
	params := service.CreateServerParams{
		Name:    c.PostForm("name"),
		OwnerID: middleware.UserID(c),
	}

	fileHeader, err := c.FormFile("icon")
	switch {
	case err == nil && fileHeader != nil:
		if fileHeader.Size > maxIconUploadBytes {
			apierr.RespondError(c, http.StatusBadRequest,
				fmt.Sprintf("icon file exceeds the maximum allowed size (%d MB)", maxIconUploadBytes>>20),
				apierr.FieldErr("icon", "icon_too_large"))
			return
		}
		data, oerr := readUpload(fileHeader)
		if oerr != nil {
			apierr.RespondError(c, http.StatusBadRequest, "could not read the icon file", apierr.FieldErr("icon", "icon_unreadable"))
			return
		}
		params.Icon = data
	case errors.Is(err, http.ErrMissingFile):
	default:
		apierr.RespondError(c, http.StatusBadRequest, "invalid multipart form", map[string]any{"reason": "invalid_multipart"})
		return
	}

	srv, err := h.uc.CreateServer(c.Request.Context(), params)
	if err != nil {
		apierr.WriteDomainError(c, err)
		return
	}
	c.JSON(http.StatusCreated, toServerResponse(srv))
}

func readUpload(fh *multipart.FileHeader) ([]byte, error) {
	f, err := fh.Open()
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, maxIconUploadBytes+1))
}
