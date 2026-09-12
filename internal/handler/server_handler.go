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
	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/apierr"
	"github.com/discordia-grupo01/servers/internal/handler/categoryview"
	"github.com/discordia-grupo01/servers/internal/handler/channelview"
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
	ListMyServers(ctx context.Context, userID string) ([]model.Server, error)
	GetServer(ctx context.Context, id uuid.UUID) (model.Server, error)
}
type IconStore interface {
	FilePath(id string) (path string, ok bool)
}

type serverHandler struct {
	uc    ServerUseCases
	icons IconStore
}

type serverResponse struct {
	ID         string                  `json:"id"`
	Name       string                  `json:"name"`
	IconURL    *string                 `json:"icon_url"`
	OwnerID    string                  `json:"owner_id"`
	CreatedAt  time.Time               `json:"created_at"`
	Channels   []channelview.Response  `json:"channels"`
	Categories []categoryview.Response `json:"categories"`
}

func toServerResponse(s model.Server) serverResponse {
	chs := make([]channelview.Response, 0, len(s.Channels))
	for _, ch := range s.Channels {
		chs = append(chs, channelview.ToResponse(ch))
	}
	cats := make([]categoryview.Response, 0, len(s.Categories))
	for _, cat := range s.Categories {
		cats = append(cats, categoryview.ToResponse(cat))
	}
	return serverResponse{
		ID: s.ID.String(), Name: s.Name, IconURL: s.IconURL, OwnerID: s.OwnerID,
		CreatedAt: s.CreatedAt, Channels: chs, Categories: cats,
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

// GET /v1/servers
func (h *serverHandler) listMyServers(c *gin.Context) {
	servers, err := h.uc.ListMyServers(c.Request.Context(), middleware.UserID(c))
	if err != nil {
		apierr.WriteDomainError(c, err)
		return
	}

	resp := make([]serverResponse, 0, len(servers))
	for _, srv := range servers {
		resp = append(resp, toServerResponse(srv))
	}
	c.JSON(http.StatusOK, resp)
}

// GET /v1/servers/:serverId
func (h *serverHandler) getServer(c *gin.Context) {
	serverID, ok := apierr.ParseUUIDParam(c, "serverId")
	if !ok {
		return
	}
	srv, err := h.uc.GetServer(c.Request.Context(), serverID)
	if err != nil {
		apierr.WriteDomainError(c, err)
		return
	}
	c.JSON(http.StatusOK, toServerResponse(srv))
}

// GET /v1/servers/:serverId/icon
func (h *serverHandler) getIcon(c *gin.Context) {
	if _, ok := apierr.ParseUUIDParam(c, "serverId"); !ok {
		return
	}
	path, ok := h.icons.FilePath(c.Param("serverId"))
	if !ok {
		apierr.RespondError(c, http.StatusNotFound, "icon not found", nil)
		return
	}
	c.File(path)
}

func readUpload(fh *multipart.FileHeader) ([]byte, error) {
	f, err := fh.Open()
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, maxIconUploadBytes+1))
}
