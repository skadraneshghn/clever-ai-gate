package admin

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/skadraneshghn/clever-ai-gate/api/dto"
	"github.com/skadraneshghn/clever-ai-gate/internal/database"
)

// TenantHandler provides CRUD operations for tenants.
type TenantHandler struct {
	db *pgxpool.Pool
}

// NewTenantHandler creates a new tenant handler.
func NewTenantHandler(db *pgxpool.Pool) *TenantHandler {
	return &TenantHandler{db: db}
}

// List returns all tenants.
// @Summary      List tenants
// @Description  Returns all registered tenants
// @Tags         Tenants
// @Produce      json
// @Security     BearerAuth
// @Success      200  {array}   dto.TenantResponse
// @Failure      500  {object}  dto.ErrorResponse
// @Router       /api/v1/admin/tenants [get]
func (h *TenantHandler) List(c *gin.Context) {
	tenants, err := database.ListTenants(c.Request.Context(), h.db)
	if err != nil {
		c.JSON(http.StatusInternalServerError, dto.ErrorResponse{Error: "failed to list tenants", Details: err.Error()})
		return
	}

	resp := make([]dto.TenantResponse, len(tenants))
	for i, t := range tenants {
		resp[i] = dto.TenantResponse{
			ID:           t.ID,
			Name:         t.Name,
			APIKey:       dto.MaskAPIKey(t.APIKey),
			TokenBalance: t.TokenBalance,
			IsActive:     t.IsActive,
			RateLimitRPM: t.RateLimitRPM,
			CreatedAt:    t.CreatedAt,
			UpdatedAt:    t.UpdatedAt,
		}
	}

	c.JSON(http.StatusOK, resp)
}

// Create creates a new tenant with a generated virtual API key.
// @Summary      Create tenant
// @Description  Creates a new tenant and generates a virtual API key
// @Tags         Tenants
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body  body      dto.CreateTenantRequest  true  "Tenant details"
// @Success      201   {object}  dto.TenantResponse
// @Failure      400   {object}  dto.ErrorResponse
// @Failure      500   {object}  dto.ErrorResponse
// @Router       /api/v1/admin/tenants [post]
func (h *TenantHandler) Create(c *gin.Context) {
	var req dto.CreateTenantRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: "invalid request body", Details: err.Error()})
		return
	}

	// Generate virtual API key: "cag_" prefix + 32 random hex chars
	apiKey, err := generateAPIKey()
	if err != nil {
		c.JSON(http.StatusInternalServerError, dto.ErrorResponse{Error: "failed to generate API key"})
		return
	}

	// Set defaults
	if req.TokenBalance == 0 {
		req.TokenBalance = 1_000_000_000 // 1 billion tokens default
	}
	if req.RateLimitRPM == 0 {
		req.RateLimitRPM = 60
	}

	id, err := database.CreateTenant(c.Request.Context(), h.db, req.Name, apiKey, req.TokenBalance, req.RateLimitRPM)
	if err != nil {
		c.JSON(http.StatusInternalServerError, dto.ErrorResponse{Error: "failed to create tenant", Details: err.Error()})
		return
	}

	// Return full API key (only shown once at creation time)
	c.JSON(http.StatusCreated, dto.TenantResponse{
		ID:           id,
		Name:         req.Name,
		APIKey:       apiKey, // Full key shown only at creation
		TokenBalance: req.TokenBalance,
		IsActive:     true,
		RateLimitRPM: req.RateLimitRPM,
	})
}

// Get returns a single tenant by ID.
// @Summary      Get tenant
// @Description  Returns a single tenant by ID
// @Tags         Tenants
// @Produce      json
// @Security     BearerAuth
// @Param        id   path      string  true  "Tenant ID (UUID)"
// @Success      200  {object}  dto.TenantResponse
// @Failure      404  {object}  dto.ErrorResponse
// @Router       /api/v1/admin/tenants/{id} [get]
func (h *TenantHandler) Get(c *gin.Context) {
	id := c.Param("id")

	// We need to query by ID, but our current queries use API key
	// For now, list and filter
	tenants, err := database.ListTenants(c.Request.Context(), h.db)
	if err != nil {
		c.JSON(http.StatusInternalServerError, dto.ErrorResponse{Error: "failed to fetch tenant"})
		return
	}

	for _, t := range tenants {
		if t.ID == id {
			c.JSON(http.StatusOK, dto.TenantResponse{
				ID:           t.ID,
				Name:         t.Name,
				APIKey:       dto.MaskAPIKey(t.APIKey),
				TokenBalance: t.TokenBalance,
				IsActive:     t.IsActive,
				RateLimitRPM: t.RateLimitRPM,
				CreatedAt:    t.CreatedAt,
				UpdatedAt:    t.UpdatedAt,
			})
			return
		}
	}

	c.JSON(http.StatusNotFound, dto.ErrorResponse{Error: "tenant not found"})
}

// Update updates a tenant's mutable fields.
// @Summary      Update tenant
// @Description  Updates a tenant's name, balance, active status, and rate limit
// @Tags         Tenants
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path      string                   true  "Tenant ID (UUID)"
// @Param        body  body      dto.UpdateTenantRequest   true  "Updated tenant details"
// @Success      200   {object}  dto.SuccessResponse
// @Failure      400   {object}  dto.ErrorResponse
// @Failure      500   {object}  dto.ErrorResponse
// @Router       /api/v1/admin/tenants/{id} [put]
func (h *TenantHandler) Update(c *gin.Context) {
	id := c.Param("id")
	var req dto.UpdateTenantRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: "invalid request body", Details: err.Error()})
		return
	}

	err := database.UpdateTenant(c.Request.Context(), h.db, id, req.Name, req.TokenBalance, req.IsActive, req.RateLimitRPM)
	if err != nil {
		c.JSON(http.StatusInternalServerError, dto.ErrorResponse{Error: "failed to update tenant", Details: err.Error()})
		return
	}

	c.JSON(http.StatusOK, dto.SuccessResponse{Message: "tenant updated successfully"})
}

// Delete removes a tenant.
// @Summary      Delete tenant
// @Description  Permanently removes a tenant
// @Tags         Tenants
// @Produce      json
// @Security     BearerAuth
// @Param        id   path      string  true  "Tenant ID (UUID)"
// @Success      200  {object}  dto.SuccessResponse
// @Failure      500  {object}  dto.ErrorResponse
// @Router       /api/v1/admin/tenants/{id} [delete]
func (h *TenantHandler) Delete(c *gin.Context) {
	id := c.Param("id")
	err := database.DeleteTenant(c.Request.Context(), h.db, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, dto.ErrorResponse{Error: "failed to delete tenant", Details: err.Error()})
		return
	}

	c.JSON(http.StatusOK, dto.SuccessResponse{Message: "tenant deleted successfully"})
}

// generateAPIKey generates a secure random API key with the "cag_" prefix.
func generateAPIKey() (string, error) {
	bytes := make([]byte, 24)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return "cag_" + hex.EncodeToString(bytes), nil
}
