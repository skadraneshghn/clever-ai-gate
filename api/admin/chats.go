package admin

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/skadraneshghn/clever-ai-gate/api/dto"
	"github.com/skadraneshghn/clever-ai-gate/internal/database"
)

// ChatHandler handles CRUD operations for playground chat conversations with Tenant isolation.
type ChatHandler struct {
	db *pgxpool.Pool
}

// NewChatHandler creates a new chat handler.
func NewChatHandler(db *pgxpool.Pool) *ChatHandler {
	return &ChatHandler{db: db}
}

// ChatListResponse represents a summarized conversation for lists.
type ChatListResponse struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// ChatDetailResponse represents full conversation details.
type ChatDetailResponse struct {
	ID        string            `json:"id"`
	Title     string            `json:"title"`
	Messages  []json.RawMessage `json:"messages"`
	CreatedAt string            `json:"created_at"`
	UpdatedAt string            `json:"updated_at"`
}

// SaveChatRequest represents the body to create/update a chat session.
type SaveChatRequest struct {
	Title    string            `json:"title" binding:"required"`
	Messages []json.RawMessage `json:"messages" binding:"required"`
}

// getTenantID returns the active tenant_id string from context.
func (h *ChatHandler) getTenantID(c *gin.Context) (string, bool) {
	tenantID, exists := c.Get("tenant_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, dto.ErrorResponse{Error: "unauthorized", Details: "missing tenant context"})
		return "", false
	}
	return tenantID.(string), true
}

// List returns all conversations in the database for the calling tenant.
func (h *ChatHandler) List(c *gin.Context) {
	tID, ok := h.getTenantID(c)
	if !ok {
		return
	}

	rows, err := database.ListConversations(c.Request.Context(), h.db, tID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, dto.ErrorResponse{Error: "failed to list conversations", Details: err.Error()})
		return
	}

	resp := make([]ChatListResponse, len(rows))
	for i, r := range rows {
		resp[i] = ChatListResponse{
			ID:        r.ID,
			Title:     r.Title,
			CreatedAt: r.CreatedAt,
			UpdatedAt: r.UpdatedAt,
		}
	}
	c.JSON(http.StatusOK, resp)
}

// Get returns full details of a single conversation session.
func (h *ChatHandler) Get(c *gin.Context) {
	tID, ok := h.getTenantID(c)
	if !ok {
		return
	}
	id := c.Param("id")

	r, err := database.GetConversation(c.Request.Context(), h.db, id, tID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, dto.ErrorResponse{Error: "failed to get conversation", Details: err.Error()})
		return
	}
	if r == nil {
		c.JSON(http.StatusNotFound, dto.ErrorResponse{Error: "conversation not found"})
		return
	}

	var messages []json.RawMessage
	if err := json.Unmarshal(r.Messages, &messages); err != nil {
		messages = []json.RawMessage{}
	}

	c.JSON(http.StatusOK, ChatDetailResponse{
		ID:        r.ID,
		Title:     r.Title,
		Messages:  messages,
		CreatedAt: r.CreatedAt,
		UpdatedAt: r.UpdatedAt,
	})
}

// Create inserts a new conversation for the calling tenant.
func (h *ChatHandler) Create(c *gin.Context) {
	tID, ok := h.getTenantID(c)
	if !ok {
		return
	}
	var req SaveChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: "invalid request body", Details: err.Error()})
		return
	}

	msgBytes, err := json.Marshal(req.Messages)
	if err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: "failed to marshal messages", Details: err.Error()})
		return
	}

	id, err := database.CreateConversation(c.Request.Context(), h.db, tID, req.Title, msgBytes)
	if err != nil {
		c.JSON(http.StatusInternalServerError, dto.ErrorResponse{Error: "failed to create conversation", Details: err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"id": id, "title": req.Title})
}

// Update modifies an existing conversation session under tenant isolation validation.
func (h *ChatHandler) Update(c *gin.Context) {
	tID, ok := h.getTenantID(c)
	if !ok {
		return
	}
	id := c.Param("id")
	var req SaveChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: "invalid request body", Details: err.Error()})
		return
	}

	msgBytes, err := json.Marshal(req.Messages)
	if err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: "failed to marshal messages", Details: err.Error()})
		return
	}

	err = database.UpdateConversation(c.Request.Context(), h.db, id, tID, req.Title, msgBytes)
	if err != nil {
		c.JSON(http.StatusInternalServerError, dto.ErrorResponse{Error: "failed to update conversation", Details: err.Error()})
		return
	}

	c.JSON(http.StatusOK, dto.SuccessResponse{Message: "conversation updated successfully"})
}

// Delete deletes a conversation session under tenant isolation validation.
func (h *ChatHandler) Delete(c *gin.Context) {
	tID, ok := h.getTenantID(c)
	if !ok {
		return
	}
	id := c.Param("id")

	err := database.DeleteConversation(c.Request.Context(), h.db, id, tID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, dto.ErrorResponse{Error: "failed to delete conversation", Details: err.Error()})
		return
	}

	c.JSON(http.StatusOK, dto.SuccessResponse{Message: "conversation deleted successfully"})
}

// GetTenantInfo returns active tenant details extracted from the authenticated request context.
// @Summary      Get Tenant Info
// @Description  Returns active tenant details extracted from the authenticated request context.
// @Tags         Playground
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  dto.TenantInfoResponse
// @Failure      401  {object}  dto.ErrorResponse
// @Router       /api/v1/playground/tenant [get]
func (h *ChatHandler) GetTenantInfo(c *gin.Context) {
	tenantID, existsID := c.Get("tenant_id")
	tenantName, _ := c.Get("tenant_name")
	tenantBalance, _ := c.Get("tenant_balance")
	tenantRateLimit, _ := c.Get("tenant_rate_limit")
	if !existsID {
		c.JSON(http.StatusUnauthorized, dto.ErrorResponse{Error: "unauthorized", Details: "missing tenant context"})
		return
	}

	idStr, _ := tenantID.(string)
	nameStr, _ := tenantName.(string)
	balanceInt, _ := tenantBalance.(int64)
	rateLimitInt, _ := tenantRateLimit.(int)

	c.JSON(http.StatusOK, dto.TenantInfoResponse{
		ID:           idStr,
		Name:         nameStr,
		TokenBalance: balanceInt,
		RateLimitRPM: rateLimitInt,
	})
}


