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
	"github.com/discordia-grupo01/servers/internal/iconstore"
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
	UpdateServer(ctx context.Context, p service.UpdateServerParams) (model.Server, error)
	GetServerImageURLs(ctx context.Context, id uuid.UUID) (iconURL, bannerURL *string, err error)
}
type IconStore interface {
	Get(key string) (data []byte, contentType string, err error)
}

type serverHandler struct {
	uc    ServerUseCases
	icons IconStore
}

type serverResponse struct {
	ID         string                  `json:"id"`
	Name       string                  `json:"name"`
	IconURL    *string                 `json:"icon_url"`
	BannerURL  *string                 `json:"banner_url"`
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
		ID: s.ID.String(), Name: s.Name, IconURL: s.IconURL, BannerURL: s.BannerURL, OwnerID: s.OwnerID,
		CreatedAt: s.CreatedAt, Channels: chs, Categories: cats,
	}
}

func extractUpload(c *gin.Context, field string) (data []byte, provided bool, ok bool) {
	fileHeader, err := c.FormFile(field)
	switch {
	case err == nil && fileHeader != nil:
		if fileHeader.Size > maxIconUploadBytes {
			apierr.RespondError(c, http.StatusBadRequest,
				fmt.Sprintf("%s file exceeds the maximum allowed size (%d MB)", field, maxIconUploadBytes>>20),
				apierr.FieldErr(field, field+"_too_large"))
			return nil, false, false
		}
		data, oerr := readUpload(fileHeader)
		if oerr != nil {
			apierr.RespondError(c, http.StatusBadRequest, "could not read the "+field+" file", apierr.FieldErr(field, field+"_unreadable"))
			return nil, false, false
		}
		return data, true, true
	case errors.Is(err, http.ErrMissingFile):
		return nil, false, true
	default:
		apierr.RespondError(c, http.StatusBadRequest, "invalid multipart form", map[string]any{"reason": "invalid_multipart"})
		return nil, false, false
	}
}

// POST /v1/servers
func (h *serverHandler) createServer(c *gin.Context) {
	params := service.CreateServerParams{
		Name:    c.PostForm("name"),
		OwnerID: middleware.UserID(c),
	}

	icon, provided, ok := extractUpload(c, "icon")
	if !ok {
		return
	}
	if provided {
		params.Icon = icon
	}

	srv, err := h.uc.CreateServer(c.Request.Context(), params)
	if err != nil {
		apierr.WriteDomainError(c, err)
		return
	}
	c.JSON(http.StatusCreated, toServerResponse(srv))
}

// PATCH /v1/servers/:serverId
func (h *serverHandler) updateServer(c *gin.Context) {
	serverID, ok := apierr.ParseUUIDParam(c, "serverId")
	if !ok {
		return
	}

	params := service.UpdateServerParams{
		ServerID:    serverID,
		ActorUserID: middleware.UserID(c),
	}
	if name, exists := c.GetPostForm("name"); exists {
		params.Name = &name
	}

	icon, provided, ok := extractUpload(c, "icon")
	if !ok {
		return
	}
	if provided {
		params.Icon = icon
	}

	banner, provided, ok := extractUpload(c, "banner")
	if !ok {
		return
	}
	if provided {
		params.Banner = banner
	}
	params.RemoveBanner = c.PostForm("remove_banner") == "true"

	srv, err := h.uc.UpdateServer(c.Request.Context(), params)
	if err != nil {
		apierr.WriteDomainError(c, err)
		return
	}
	c.JSON(http.StatusOK, toServerResponse(srv))
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
	serverID, ok := apierr.ParseUUIDParam(c, "serverId")
	if !ok {
		return
	}
	iconURL, _, err := h.uc.GetServerImageURLs(c.Request.Context(), serverID)
	if err != nil {
		apierr.WriteDomainError(c, err)
		return
	}
	if iconURL == nil {
		apierr.RespondError(c, http.StatusNotFound, "icon not found", nil)
		return
	}
	hash, ok := iconstore.HashFromURL(*iconURL)
	if !ok {
		apierr.RespondError(c, http.StatusInternalServerError, "malformed icon url", nil)
		return
	}
	data, contentType, err := h.icons.Get(iconstore.StorageKey(serverID.String(), hash))
	if err != nil {
		if errors.Is(err, model.ErrIconNotFound) {
			apierr.RespondError(c, http.StatusNotFound, "icon not found", nil)
			return
		}
		apierr.RespondError(c, http.StatusInternalServerError, "could not read icon", nil)
		return
	}
	c.Data(http.StatusOK, contentType, data)
}

// GET /v1/servers/:serverId/banner
func (h *serverHandler) getBanner(c *gin.Context) {
	serverID, ok := apierr.ParseUUIDParam(c, "serverId")
	if !ok {
		return
	}
	_, bannerURL, err := h.uc.GetServerImageURLs(c.Request.Context(), serverID)
	if err != nil {
		apierr.WriteDomainError(c, err)
		return
	}
	if bannerURL == nil {
		apierr.RespondError(c, http.StatusNotFound, "banner not found", nil)
		return
	}
	hash, ok := iconstore.HashFromURL(*bannerURL)
	if !ok {
		apierr.RespondError(c, http.StatusInternalServerError, "malformed banner url", nil)
		return
	}
	data, contentType, err := h.icons.Get(iconstore.StorageKey(service.BannerObjectID(serverID), hash))
	if err != nil {
		if errors.Is(err, model.ErrIconNotFound) {
			apierr.RespondError(c, http.StatusNotFound, "banner not found", nil)
			return
		}
		apierr.RespondError(c, http.StatusInternalServerError, "could not read banner", nil)
		return
	}
	c.Data(http.StatusOK, contentType, data)
}

func readUpload(fh *multipart.FileHeader) ([]byte, error) {
	f, err := fh.Open()
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, maxIconUploadBytes+1))
}
