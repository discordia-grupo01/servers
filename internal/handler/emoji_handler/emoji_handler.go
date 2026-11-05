package emoji_handler

import (
	"context"
	"errors"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/discordia-grupo01/servers/internal/apierr"
	"github.com/discordia-grupo01/servers/internal/middleware"
	"github.com/discordia-grupo01/servers/internal/model"
	"github.com/discordia-grupo01/servers/internal/model/emoji"
	"github.com/discordia-grupo01/servers/internal/service/emoji_service"
)

const multipartOverhead = 1 << 20

type UseCases interface {
	Create(ctx context.Context, p emoji_service.CreateParams) (emoji.Emoji, error)
	List(ctx context.Context, p emoji_service.ListParams) (emoji.Page, error)
	Delete(ctx context.Context, p emoji_service.DeleteParams) error
}

type ImageReader interface {
	GetEmoji(serverID, key string) (data []byte, contentType string, err error)
}

type Handler struct {
	uc     UseCases
	images ImageReader
}

func New(uc UseCases, images ImageReader) *Handler {
	return &Handler{uc: uc, images: images}
}

// Create godoc
// @Summary      Add a custom emoji to a server
// @Description  Requires MANAGE_SERVER (the owner and administrators hold it implicitly). Multipart form with name (2 to 32 letters, numbers or underscores, unique in the server ignoring case) and image (PNG, WebP or GIF, at most 5 MB, detected from the file's content). A GIF creates an animated emoji. A server has 50 slots, 70 from boost level 1; deleted emojis free theirs. A server that drops a level keeps the emojis it has, but can't add more until it is under the limit.
// @Tags         emojis
// @Accept       multipart/form-data
// @Produce      json
// @Security     BearerAuth
// @Param        serverId  path      string  true  "Server ID"
// @Param        name      formData  string  true  "Emoji name"
// @Param        image     formData  file    true  "Emoji image"
// @Success      201  {object}  emoji.Response
// @Failure      400  {object}  apierr.ErrorResponse  "invalid name, or image missing, too large or of an unsupported type"
// @Failure      401  {object}  apierr.ErrorResponse
// @Failure      403  {object}  apierr.ErrorResponse  "missing MANAGE_SERVER"
// @Failure      404  {object}  apierr.ErrorResponse  "server not found"
// @Failure      409  {object}  apierr.ErrorResponse  "emoji_name_taken or emoji_limit_reached"
// @Router       /v1/servers/{serverId}/emojis [post]
func (h *Handler) Create(c *gin.Context) {
	serverID, ok := apierr.ParseUUIDParam(c, "serverId")
	if !ok {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, emoji.MaxImageBytes+multipartOverhead)
	image, ok := readImage(c)
	if !ok {
		return
	}
	created, err := h.uc.Create(c.Request.Context(), emoji_service.CreateParams{
		ServerID:    serverID,
		ActorUserID: middleware.UserID(c),
		Name:        c.PostForm("name"),
		Image:       image,
	})
	if err != nil {
		apierr.WriteDomainError(c, err)
		return
	}
	c.JSON(http.StatusCreated, emoji.ToResponse(created))
}

// List godoc
// @Summary      List a server's custom emojis
// @Description  Any member of the server. Oldest first, without the deleted ones. max_emojis is the number of slots the server has.
// @Tags         emojis
// @Produce      json
// @Security     BearerAuth
// @Param        serverId  path  string  true  "Server ID"
// @Success      200  {object}  emoji.ListResponse
// @Failure      401  {object}  apierr.ErrorResponse
// @Failure      403  {object}  apierr.ErrorResponse  "not a member of the server"
// @Failure      404  {object}  apierr.ErrorResponse  "server not found"
// @Router       /v1/servers/{serverId}/emojis [get]
func (h *Handler) List(c *gin.Context) {
	serverID, ok := apierr.ParseUUIDParam(c, "serverId")
	if !ok {
		return
	}
	page, err := h.uc.List(c.Request.Context(), emoji_service.ListParams{
		ServerID:    serverID,
		ActorUserID: middleware.UserID(c),
	})
	if err != nil {
		apierr.WriteDomainError(c, err)
		return
	}
	c.JSON(http.StatusOK, emoji.ToListResponse(page))
}

// Delete godoc
// @Summary      Delete a custom emoji
// @Description  Requires MANAGE_SERVER. The emoji can't be used in new messages or reactions and its slot and name become free, but its image is kept so the messages and reactions that already use it still show it.
// @Tags         emojis
// @Produce      json
// @Security     BearerAuth
// @Param        serverId  path  string  true  "Server ID"
// @Param        emojiId   path  string  true  "Emoji ID"
// @Success      204  "deleted"
// @Failure      400  {object}  apierr.ErrorResponse  "invalid id"
// @Failure      401  {object}  apierr.ErrorResponse
// @Failure      403  {object}  apierr.ErrorResponse  "missing MANAGE_SERVER"
// @Failure      404  {object}  apierr.ErrorResponse  "server or emoji not found"
// @Router       /v1/servers/{serverId}/emojis/{emojiId} [delete]
func (h *Handler) Delete(c *gin.Context) {
	serverID, ok := apierr.ParseUUIDParam(c, "serverId")
	if !ok {
		return
	}
	emojiID, ok := apierr.ParseUUIDParam(c, "emojiId")
	if !ok {
		return
	}
	err := h.uc.Delete(c.Request.Context(), emoji_service.DeleteParams{
		ServerID:    serverID,
		EmojiID:     emojiID,
		ActorUserID: middleware.UserID(c),
	})
	if err != nil {
		apierr.WriteDomainError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// GetImage serves emoji images in local development. Production images live in
// a public bucket, so this route isn't registered there.
func (h *Handler) GetImage(c *gin.Context) {
	serverID, ok := apierr.ParseUUIDParam(c, "serverId")
	if !ok {
		return
	}
	data, contentType, err := h.images.GetEmoji(serverID.String(), c.Param("imageKey"))
	if err != nil {
		apierr.WriteDomainError(c, err)
		return
	}
	c.Data(http.StatusOK, contentType, data)
}

func readImage(c *gin.Context) ([]byte, bool) {
	header, err := c.FormFile("image")
	if errors.Is(err, http.ErrMissingFile) {
		return nil, true
	}
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			apierr.WriteDomainError(c, model.ErrEmojiImageTooLarge)
			return nil, false
		}
		apierr.RespondError(c, http.StatusBadRequest, "invalid multipart form", map[string]any{"reason": "invalid_multipart"})
		return nil, false
	}
	file, err := header.Open()
	if err != nil {
		apierr.RespondError(c, http.StatusBadRequest, "could not read the image file", apierr.FieldErr("image", "emoji_image_unreadable"))
		return nil, false
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, emoji.MaxImageBytes+1))
	if err != nil {
		apierr.RespondError(c, http.StatusBadRequest, "could not read the image file", apierr.FieldErr("image", "emoji_image_unreadable"))
		return nil, false
	}
	return data, true
}
