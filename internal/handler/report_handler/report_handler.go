package report_handler

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/discordia-grupo01/servers/internal/apierr"
	"github.com/discordia-grupo01/servers/internal/middleware"
	"github.com/discordia-grupo01/servers/internal/model/report"
	"github.com/discordia-grupo01/servers/internal/service/report_service"
)

type UseCases interface {
	Create(ctx context.Context, p report_service.CreateParams) (report.Report, error)
	List(ctx context.Context, p report_service.ListParams) (report.Page, error)
	Close(ctx context.Context, p report_service.CloseParams) (report.Report, error)
}

type Handler struct {
	uc UseCases
}

func New(uc UseCases) *Handler {
	return &Handler{uc: uc}
}

type CreateRequest struct {
	Kind      string `json:"kind" example:"message" enums:"message,user"`
	ChannelID string `json:"channel_id" example:"0b6f6a2e-6a41-4a42-9d0a-2c6a2d1f7c11"`
	MessageID string `json:"message_id" example:"01a1b2c3-d4e5-7f60-8a9b-0c1d2e3f4a5b"`
	UserID    string `json:"user_id" example:"a3f1c0de-1111-4222-8333-444455556666"`
	Reason    string `json:"reason" example:"Insultos repetidos hacia otros miembros"`
}

type CloseRequest struct {
	Status string `json:"status" example:"resolved" enums:"resolved,dismissed"`
	Action string `json:"action" example:"mute" enums:"delete_message,mute,kick,ban"`
}

// Create godoc
// @Summary      Report a message or a user
// @Description  Any member of the server can report. With kind "message" send channel_id and message_id (the channel must be a text channel of the server that the reporter can see); with kind "user" send user_id (a current member, not the reporter). The reason is required, at most 512 characters. Members with moderation permissions (MANAGE_MESSAGES, KICK_MEMBERS and BAN_MEMBERS, held together; the owner and administrators hold them implicitly) review it through GET /v1/servers/{serverId}/reports. The server cannot check that the message exists: messaging owns it. A reporter can't have two pending reports about the same message or user (409 report_already_pending); once it is closed they can report it again.
// @Tags         reports
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        serverId  path  string         true  "Server ID"
// @Param        body      body  CreateRequest  true  "What is reported and why"
// @Success      201  {object}  report.Response
// @Failure      400  {object}  apierr.ErrorResponse  "invalid kind, missing target or reason, reason too long, not a text channel, or reporting yourself"
// @Failure      401  {object}  apierr.ErrorResponse
// @Failure      403  {object}  apierr.ErrorResponse  "not a member of the server"
// @Failure      404  {object}  apierr.ErrorResponse  "server, channel or reported user not found"
// @Failure      409  {object}  apierr.ErrorResponse  "report_already_pending"
// @Router       /v1/servers/{serverId}/reports [post]
func (h *Handler) Create(c *gin.Context) {
	serverID, ok := apierr.ParseUUIDParam(c, "serverId")
	if !ok {
		return
	}
	var req CreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apierr.RespondError(c, http.StatusBadRequest, "invalid request body", map[string]any{"reason": "invalid_json"})
		return
	}
	channelID, ok := parseOptionalUUID(c, "channel_id", req.ChannelID)
	if !ok {
		return
	}
	messageID, ok := parseOptionalUUID(c, "message_id", req.MessageID)
	if !ok {
		return
	}
	created, err := h.uc.Create(c.Request.Context(), report_service.CreateParams{
		ServerID:    serverID,
		ActorUserID: middleware.UserID(c),
		Kind:        req.Kind,
		ChannelID:   channelID,
		MessageID:   messageID,
		UserID:      req.UserID,
		Reason:      req.Reason,
	})
	if err != nil {
		apierr.WriteDomainError(c, err)
		return
	}
	c.JSON(http.StatusCreated, report.ToResponse(created))
}

// List godoc
// @Summary      List the server's reports
// @Description  Only for moderators: MANAGE_MESSAGES, KICK_MEMBERS and BAN_MEMBERS held together (the owner and administrators hold them implicitly). Newest first. status filters by pending (default), resolved, dismissed or all; "total" is the number of reports with that status, so GET ?status=pending&limit=1 gives the pending count. Each report carries the replicated profile of the reporter and, for a user report, of the reported user (null when it hasn't arrived yet).
// @Tags         reports
// @Produce      json
// @Security     BearerAuth
// @Param        serverId  path   string  true   "Server ID"
// @Param        status    query  string  false  "pending (default), resolved, dismissed or all"
// @Param        limit     query  int     false  "Page size (default 20, max 100)"
// @Param        offset    query  int     false  "Reports to skip"
// @Success      200  {object}  report.ListResponse
// @Failure      400  {object}  apierr.ErrorResponse  "invalid status, limit or offset"
// @Failure      401  {object}  apierr.ErrorResponse
// @Failure      403  {object}  apierr.ErrorResponse  "not a member, or not a moderator"
// @Failure      404  {object}  apierr.ErrorResponse  "server not found"
// @Router       /v1/servers/{serverId}/reports [get]
func (h *Handler) List(c *gin.Context) {
	serverID, ok := apierr.ParseUUIDParam(c, "serverId")
	if !ok {
		return
	}
	limit, ok := apierr.ParseNonNegativeIntQuery(c, "limit", 0)
	if !ok {
		return
	}
	offset, ok := apierr.ParseNonNegativeIntQuery(c, "offset", 0)
	if !ok {
		return
	}
	page, err := h.uc.List(c.Request.Context(), report_service.ListParams{
		ServerID:    serverID,
		ActorUserID: middleware.UserID(c),
		Status:      c.Query("status"),
		Limit:       limit,
		Offset:      offset,
	})
	if err != nil {
		apierr.WriteDomainError(c, err)
		return
	}
	c.JSON(http.StatusOK, report.ToListResponse(page))
}

// Close godoc
// @Summary      Close a pending report
// @Description  Only for moderators (see GET /v1/servers/{serverId}/reports). Marks the report as resolved (the moderator acted on it) or dismissed (nothing to act on). A report is closed once: closing it again is a 409 report_not_pending. Acting on the reported message or user is done with the existing moderation endpoints; when resolving, the optional action records which one was used (delete_message, mute, kick or ban; delete_message only for a message report) and comes back as resolution_action. The server doesn't check that the action happened.
// @Tags         reports
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        serverId  path  string        true  "Server ID"
// @Param        reportId  path  string        true  "Report ID"
// @Param        body      body  CloseRequest  true  "The new status"
// @Success      200  {object}  report.Response
// @Failure      400  {object}  apierr.ErrorResponse  "status is not resolved or dismissed, action is unknown or sent with dismissed (action_invalid), or delete_message on a user report (action_not_for_kind)"
// @Failure      401  {object}  apierr.ErrorResponse
// @Failure      403  {object}  apierr.ErrorResponse  "not a member, or not a moderator"
// @Failure      404  {object}  apierr.ErrorResponse  "server or report not found"
// @Failure      409  {object}  apierr.ErrorResponse  "report_not_pending"
// @Router       /v1/servers/{serverId}/reports/{reportId} [patch]
func (h *Handler) Close(c *gin.Context) {
	serverID, ok := apierr.ParseUUIDParam(c, "serverId")
	if !ok {
		return
	}
	reportID, ok := apierr.ParseUUIDParam(c, "reportId")
	if !ok {
		return
	}
	var req CloseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apierr.RespondError(c, http.StatusBadRequest, "invalid request body", map[string]any{"reason": "invalid_json"})
		return
	}
	closed, err := h.uc.Close(c.Request.Context(), report_service.CloseParams{
		ServerID:    serverID,
		ReportID:    reportID,
		ActorUserID: middleware.UserID(c),
		Status:      req.Status,
		Action:      req.Action,
	})
	if err != nil {
		apierr.WriteDomainError(c, err)
		return
	}
	c.JSON(http.StatusOK, report.ToResponse(closed))
}

func parseOptionalUUID(c *gin.Context, field, raw string) (*uuid.UUID, bool) {
	if raw == "" {
		return nil, true
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		apierr.RespondError(c, http.StatusBadRequest, "invalid "+field, apierr.FieldErr(field, "invalid_uuid"))
		return nil, false
	}
	return &id, true
}
