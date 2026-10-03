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
	DeleteServer(ctx context.Context, p service.DeleteServerParams) error
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

// createServer godoc
// @Summary      Create a server
// @Description  Creates a server owned by the caller (identity from the JWT). Multipart form body.
// @Tags         servers
// @Accept       multipart/form-data
// @Produce      json
// @Security     BearerAuth
// @Param        name  formData  string  true   "Server name"
// @Param        icon  formData  file    false  "Server icon image"
// @Success      201  {object}  serverResponse
// @Failure      400  {object}  apierr.ErrorResponse  "invalid name, or icon too large/unreadable"
// @Failure      401  {object}  apierr.ErrorResponse
// @Failure      409  {object}  apierr.ErrorResponse  "name already taken"
// @Router       /v1/servers [post]
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

// updateServer godoc
// @Summary      Edit a server
// @Description  Administrators only (the owner, or a member with ADMINISTRATOR). Multipart form, all fields optional: rename, replace the icon/banner, or remove the banner.
// @Tags         servers
// @Accept       multipart/form-data
// @Produce      json
// @Security     BearerAuth
// @Param        serverId       path      string  true   "Server ID"
// @Param        name           formData  string  false  "New server name"
// @Param        icon           formData  file    false  "New icon image"
// @Param        banner         formData  file    false  "New banner image"
// @Param        remove_banner  formData  string  false  "Pass \"true\" to remove the current banner"
// @Success      200  {object}  serverResponse
// @Failure      400  {object}  apierr.ErrorResponse
// @Failure      401  {object}  apierr.ErrorResponse
// @Failure      403  {object}  apierr.ErrorResponse  "not a server administrator"
// @Failure      404  {object}  apierr.ErrorResponse
// @Failure      409  {object}  apierr.ErrorResponse  "name already taken"
// @Router       /v1/servers/{serverId} [patch]
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

type deleteServerRequest struct {
	ConfirmName string `json:"confirm_name" example:"Mi Comunidad"`
}

// deleteServer godoc
// @Summary      Delete a server
// @Description  Permanently deletes the server with its channels, categories, roles, members, invitations, bans and messages. Only the owner can do it, and only after confirming by sending the server's exact name in confirm_name. Not reversible.
// @Tags         servers
// @Accept       json
// @Security     BearerAuth
// @Param        serverId  path  string               true  "Server id (UUID)"
// @Param        body      body  deleteServerRequest  true  "Confirmation"
// @Success      204  "deleted"
// @Failure      400  {object}  apierr.ErrorResponse  "invalid body, or confirm_name missing or different from the server name"
// @Failure      401  {object}  apierr.ErrorResponse
// @Failure      403  {object}  apierr.ErrorResponse  "not the server owner"
// @Failure      404  {object}  apierr.ErrorResponse
// @Router       /v1/servers/{serverId} [delete]
func (h *serverHandler) deleteServer(c *gin.Context) {
	serverID, ok := apierr.ParseUUIDParam(c, "serverId")
	if !ok {
		return
	}

	var body deleteServerRequest
	if err := c.ShouldBindJSON(&body); err != nil && !errors.Is(err, io.EOF) {
		apierr.RespondError(c, http.StatusBadRequest, "invalid request body", map[string]any{"reason": "invalid_json"})
		return
	}

	err := h.uc.DeleteServer(c.Request.Context(), service.DeleteServerParams{
		ServerID:    serverID,
		ActorUserID: middleware.UserID(c),
		ConfirmName: body.ConfirmName,
	})
	if err != nil {
		apierr.WriteDomainError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// listMyServers godoc
// @Summary      List my servers
// @Description  Servers the caller is a member of.
// @Tags         servers
// @Produce      json
// @Security     BearerAuth
// @Success      200  {array}   serverResponse
// @Failure      401  {object}  apierr.ErrorResponse
// @Router       /v1/servers [get]
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

// getServer godoc
// @Summary      Get a server
// @Description  Full detail of one server, including its channels and categories.
// @Tags         servers
// @Produce      json
// @Security     BearerAuth
// @Param        serverId  path  string  true  "Server ID"
// @Success      200  {object}  serverResponse
// @Failure      400  {object}  apierr.ErrorResponse  "malformed serverId"
// @Failure      404  {object}  apierr.ErrorResponse
// @Router       /v1/servers/{serverId} [get]
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

// getIcon godoc
// @Summary      Get a server's icon
// @Description  Public (no JWT required) -- binary image, so a plain `<img src>` can load it.
// @Tags         servers
// @Produce      png
// @Param        serverId  path  string  true  "Server ID"
// @Success      200  {file}    file  "the icon image"
// @Failure      400  {object}  apierr.ErrorResponse
// @Failure      404  {object}  apierr.ErrorResponse  "server, or icon, not found"
// @Router       /v1/servers/{serverId}/icon [get]
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

	key := serverID.String()
	if hash, ok := iconstore.HashFromURL(*iconURL); ok {
		key = iconstore.StorageKey(serverID.String(), hash)
	}
	data, contentType, err := h.icons.Get(key)
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

// getBanner godoc
// @Summary      Get a server's banner
// @Description  Public (no JWT required) -- binary image, so a plain `<img src>` can load it.
// @Tags         servers
// @Produce      png
// @Param        serverId  path  string  true  "Server ID"
// @Success      200  {file}    file  "the banner image"
// @Failure      400  {object}  apierr.ErrorResponse
// @Failure      404  {object}  apierr.ErrorResponse  "server has no banner, or not found"
// @Router       /v1/servers/{serverId}/banner [get]
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

	key := service.BannerObjectID(serverID)
	if hash, ok := iconstore.HashFromURL(*bannerURL); ok {
		key = iconstore.StorageKey(key, hash)
	}
	data, contentType, err := h.icons.Get(key)
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
