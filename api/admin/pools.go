package admin

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/skadraneshghn/clever-ai-gate/api/dto"
	"github.com/skadraneshghn/clever-ai-gate/internal/database"
)

// PoolHandler provides CRUD operations for model routing pools.
type PoolHandler struct {
	db *pgxpool.Pool
}

// NewPoolHandler creates a new pool handler.
func NewPoolHandler(db *pgxpool.Pool) *PoolHandler {
	return &PoolHandler{db: db}
}

// List returns all model routing pools.
// @Summary      List model pools
// @Description  Returns all model routing pools with credential counts
// @Tags         Pools
// @Produce      json
// @Security     BearerAuth
// @Success      200  {array}   dto.PoolResponse
// @Failure      500  {object}  dto.ErrorResponse
// @Router       /api/v1/admin/pools [get]
func (h *PoolHandler) List(c *gin.Context) {
	pools, err := database.ListModelPools(c.Request.Context(), h.db)
	if err != nil {
		c.JSON(http.StatusInternalServerError, dto.ErrorResponse{Error: "failed to list pools", Details: err.Error()})
		return
	}

	resp := make([]dto.PoolResponse, len(pools))
	for i, p := range pools {
		// Get credential count
		creds, _ := database.ListCredentialsByPool(c.Request.Context(), h.db, p.ID)

		// Decode capabilities JSONB into map
		var caps map[string]bool
		if len(p.Capabilities) > 0 {
			_ = json.Unmarshal(p.Capabilities, &caps)
		}

		resp[i] = dto.PoolResponse{
			ID:              p.ID,
			ModelPattern:    p.ModelPattern,
			Strategy:        p.Strategy,
			FallbackPoolID:  p.FallbackPoolID,
			Capabilities:    caps,
			CredentialCount: len(creds),
			CreatedAt:       p.CreatedAt,
		}
	}

	c.JSON(http.StatusOK, resp)
}

// Get returns a single pool with all its credentials.
// @Summary      Get model pool
// @Description  Returns a model pool with all associated credentials
// @Tags         Pools
// @Produce      json
// @Security     BearerAuth
// @Param        id   path      int  true  "Pool ID"
// @Success      200  {object}  dto.PoolResponse
// @Failure      404  {object}  dto.ErrorResponse
// @Router       /api/v1/admin/pools/{id} [get]
func (h *PoolHandler) Get(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: "invalid pool ID"})
		return
	}

	pool, err := database.GetModelPool(c.Request.Context(), h.db, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, dto.ErrorResponse{Error: "failed to get pool"})
		return
	}
	if pool == nil {
		c.JSON(http.StatusNotFound, dto.ErrorResponse{Error: "pool not found"})
		return
	}

	// Get credentials
	creds, _ := database.ListCredentialsByPool(c.Request.Context(), h.db, id)
	credResponses := make([]dto.CredentialResponse, len(creds))
	for i, cr := range creds {
		credResponses[i] = dto.CredentialResponse{
			ID:        cr.ID,
			PoolID:    cr.PoolID,
			Provider:  cr.Provider,
			BaseURL:   cr.BaseURL,
			Weight:    cr.Weight,
			IsHealthy: cr.IsHealthy,
			LastError: cr.LastError,
			KeyMask:   dto.MaskAPIKey(cr.EncryptedKey), // Show mask of encrypted key
			CreatedAt: cr.CreatedAt,
		}
	}

	// Decode capabilities JSONB
	var caps map[string]bool
	if len(pool.Capabilities) > 0 {
		_ = json.Unmarshal(pool.Capabilities, &caps)
	}

	c.JSON(http.StatusOK, dto.PoolResponse{
		ID:              pool.ID,
		ModelPattern:    pool.ModelPattern,
		Strategy:        pool.Strategy,
		FallbackPoolID:  pool.FallbackPoolID,
		Capabilities:    caps,
		CredentialCount: len(creds),
		Credentials:     credResponses,
		CreatedAt:       pool.CreatedAt,
	})
}

// Create creates a new model routing pool.
// @Summary      Create model pool
// @Description  Creates a new model routing pool
// @Tags         Pools
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body  body      dto.CreatePoolRequest  true  "Pool details"
// @Success      201   {object}  dto.PoolResponse
// @Failure      400   {object}  dto.ErrorResponse
// @Failure      500   {object}  dto.ErrorResponse
// @Router       /api/v1/admin/pools [post]
func (h *PoolHandler) Create(c *gin.Context) {
	var req dto.CreatePoolRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: "invalid request body", Details: err.Error()})
		return
	}

	if req.Strategy == "" {
		req.Strategy = "round-robin"
	}

	id, err := database.CreateModelPool(c.Request.Context(), h.db, req.ModelPattern, req.Strategy, req.FallbackPoolID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, dto.ErrorResponse{Error: "failed to create pool", Details: err.Error()})
		return
	}

	c.JSON(http.StatusCreated, dto.PoolResponse{
		ID:             id,
		ModelPattern:   req.ModelPattern,
		Strategy:       req.Strategy,
		FallbackPoolID: req.FallbackPoolID,
	})
}

// Update updates a model pool's configuration.
// @Summary      Update model pool
// @Description  Updates a model pool's pattern, strategy, and fallback
// @Tags         Pools
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path      int                    true  "Pool ID"
// @Param        body  body      dto.UpdatePoolRequest  true  "Updated pool details"
// @Success      200   {object}  dto.SuccessResponse
// @Failure      400   {object}  dto.ErrorResponse
// @Failure      500   {object}  dto.ErrorResponse
// @Router       /api/v1/admin/pools/{id} [put]
func (h *PoolHandler) Update(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: "invalid pool ID"})
		return
	}

	var req dto.UpdatePoolRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: "invalid request body", Details: err.Error()})
		return
	}

	err = database.UpdateModelPool(c.Request.Context(), h.db, id, req.ModelPattern, req.Strategy, req.FallbackPoolID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, dto.ErrorResponse{Error: "failed to update pool", Details: err.Error()})
		return
	}

	c.JSON(http.StatusOK, dto.SuccessResponse{Message: "pool updated successfully"})
}

// Delete removes a model pool and all its credentials.
// @Summary      Delete model pool
// @Description  Permanently removes a pool and cascades to its credentials
// @Tags         Pools
// @Produce      json
// @Security     BearerAuth
// @Param        id   path      int  true  "Pool ID"
// @Success      200  {object}  dto.SuccessResponse
// @Failure      500  {object}  dto.ErrorResponse
// @Router       /api/v1/admin/pools/{id} [delete]
func (h *PoolHandler) Delete(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: "invalid pool ID"})
		return
	}

	err = database.DeleteModelPool(c.Request.Context(), h.db, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, dto.ErrorResponse{Error: "failed to delete pool", Details: err.Error()})
		return
	}

	c.JSON(http.StatusOK, dto.SuccessResponse{Message: "pool deleted successfully"})
}
