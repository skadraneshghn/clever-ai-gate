package admin

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/skadraneshghn/clever-ai-gate/api/dto"
	"github.com/skadraneshghn/clever-ai-gate/internal/credentials"
	"github.com/skadraneshghn/clever-ai-gate/internal/database"
)

// CredentialHandler provides CRUD operations for provider credentials.
type CredentialHandler struct {
	db    *pgxpool.Pool
	vault *credentials.Vault
}

// NewCredentialHandler creates a new credential handler.
func NewCredentialHandler(db *pgxpool.Pool, vault *credentials.Vault) *CredentialHandler {
	return &CredentialHandler{db: db, vault: vault}
}

// Create adds a new provider credential to a pool.
// @Summary      Add credential
// @Description  Adds a new provider API key to a model routing pool
// @Tags         Credentials
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body  body      dto.CreateCredentialRequest  true  "Credential details"
// @Success      201   {object}  dto.CredentialResponse
// @Failure      400   {object}  dto.ErrorResponse
// @Failure      500   {object}  dto.ErrorResponse
// @Router       /api/v1/admin/credentials [post]
func (h *CredentialHandler) Create(c *gin.Context) {
	var req dto.CreateCredentialRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: "invalid request body", Details: err.Error()})
		return
	}

	if req.Weight <= 0 {
		req.Weight = 1
	}

	// Validate pool exists
	pool, err := database.GetModelPool(c.Request.Context(), h.db, req.PoolID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, dto.ErrorResponse{Error: "failed to validate pool"})
		return
	}
	if pool == nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: "pool not found", Details: "the specified pool_id does not exist"})
		return
	}

	// Encrypt API key before storage
	encryptedKey, err := h.vault.Encrypt(req.APIKey)
	if err != nil {
		c.JSON(http.StatusInternalServerError, dto.ErrorResponse{Error: "failed to encrypt API key"})
		return
	}

	id, err := database.CreateCredential(c.Request.Context(), h.db, req.PoolID, req.Provider, encryptedKey, req.BaseURL, req.Weight)
	if err != nil {
		c.JSON(http.StatusInternalServerError, dto.ErrorResponse{Error: "failed to create credential", Details: err.Error()})
		return
	}

	c.JSON(http.StatusCreated, dto.CredentialResponse{
		ID:        id,
		PoolID:    req.PoolID,
		Provider:  req.Provider,
		BaseURL:   req.BaseURL,
		Weight:    req.Weight,
		IsHealthy: true,
		KeyMask:   dto.MaskAPIKey(req.APIKey),
	})
}

// Get returns a single credential with masked key.
// @Summary      Get credential
// @Description  Returns a credential by ID with masked API key
// @Tags         Credentials
// @Produce      json
// @Security     BearerAuth
// @Param        id   path      int  true  "Credential ID"
// @Success      200  {object}  dto.CredentialResponse
// @Failure      404  {object}  dto.ErrorResponse
// @Router       /api/v1/admin/credentials/{id} [get]
func (h *CredentialHandler) Get(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: "invalid credential ID"})
		return
	}

	cred, err := database.GetCredential(c.Request.Context(), h.db, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, dto.ErrorResponse{Error: "failed to get credential"})
		return
	}
	if cred == nil {
		c.JSON(http.StatusNotFound, dto.ErrorResponse{Error: "credential not found"})
		return
	}

	c.JSON(http.StatusOK, dto.CredentialResponse{
		ID:        cred.ID,
		PoolID:    cred.PoolID,
		Provider:  cred.Provider,
		BaseURL:   cred.BaseURL,
		Weight:    cred.Weight,
		IsHealthy: cred.IsHealthy,
		LastError: cred.LastError,
		KeyMask:   dto.MaskAPIKey(cred.EncryptedKey),
		CreatedAt: cred.CreatedAt,
	})
}

// Update updates a credential's configuration.
// @Summary      Update credential
// @Description  Updates a credential's provider, key, URL, weight, and health
// @Tags         Credentials
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path      int                         true  "Credential ID"
// @Param        body  body      dto.UpdateCredentialRequest  true  "Updated credential details"
// @Success      200   {object}  dto.SuccessResponse
// @Failure      400   {object}  dto.ErrorResponse
// @Failure      500   {object}  dto.ErrorResponse
// @Router       /api/v1/admin/credentials/{id} [put]
func (h *CredentialHandler) Update(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: "invalid credential ID"})
		return
	}

	var req dto.UpdateCredentialRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: "invalid request body", Details: err.Error()})
		return
	}

	// If API key is provided, encrypt it
	encryptedKey := ""
	if req.APIKey != "" {
		encrypted, err := h.vault.Encrypt(req.APIKey)
		if err != nil {
			c.JSON(http.StatusInternalServerError, dto.ErrorResponse{Error: "failed to encrypt API key"})
			return
		}
		encryptedKey = encrypted
	} else {
		// Keep existing key
		existing, err := database.GetCredential(c.Request.Context(), h.db, id)
		if err != nil || existing == nil {
			c.JSON(http.StatusNotFound, dto.ErrorResponse{Error: "credential not found"})
			return
		}
		encryptedKey = existing.EncryptedKey
	}

	err = database.UpdateCredential(c.Request.Context(), h.db, id, req.Provider, encryptedKey, req.BaseURL, req.Weight, req.IsHealthy)
	if err != nil {
		c.JSON(http.StatusInternalServerError, dto.ErrorResponse{Error: "failed to update credential", Details: err.Error()})
		return
	}

	c.JSON(http.StatusOK, dto.SuccessResponse{Message: "credential updated successfully"})
}

// Delete removes a credential.
// @Summary      Delete credential
// @Description  Permanently removes a provider credential
// @Tags         Credentials
// @Produce      json
// @Security     BearerAuth
// @Param        id   path      int  true  "Credential ID"
// @Success      200  {object}  dto.SuccessResponse
// @Failure      500  {object}  dto.ErrorResponse
// @Router       /api/v1/admin/credentials/{id} [delete]
func (h *CredentialHandler) Delete(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: "invalid credential ID"})
		return
	}

	err = database.DeleteCredential(c.Request.Context(), h.db, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, dto.ErrorResponse{Error: "failed to delete credential", Details: err.Error()})
		return
	}

	c.JSON(http.StatusOK, dto.SuccessResponse{Message: "credential deleted successfully"})
}

// RegisterNvidiaProvider auto-discovers all models available under an NVIDIA API key,
// creates model pools for each, and binds the credential to all of them in one transaction.
// @Summary      Auto-discover NVIDIA Models
// @Description  Submits an NVIDIA key, hits their /models endpoint, and registers all active models automatically
// @Tags         Credentials
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body  body      dto.DiscoverProviderRequest  true  "NVIDIA provider details"
// @Success      200   {object}  dto.DiscoverProviderResponse
// @Failure      400   {object}  dto.ErrorResponse
// @Failure      500   {object}  dto.ErrorResponse
// @Router       /api/v1/admin/providers/nvidia [post]
func (h *CredentialHandler) RegisterNvidiaProvider(c *gin.Context) {
	var req dto.DiscoverProviderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: "invalid request body", Details: err.Error()})
		return
	}

	if req.Provider != "nvidia" {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: "invalid provider, must be 'nvidia'"})
		return
	}

	if req.Weight <= 0 {
		req.Weight = 1
	}

	count, models, err := credentials.DiscoverAndRegisterNvidiaModels(
		c.Request.Context(),
		h.db,
		h.vault,
		req.APIKey,
		req.BaseURL,
		req.Weight,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, dto.ErrorResponse{Error: "NVIDIA auto-discovery failed", Details: err.Error()})
		return
	}

	c.JSON(http.StatusOK, dto.DiscoverProviderResponse{
		Message:       "Successfully synchronized all NVIDIA models",
		ModelsCount:   count,
		DiscoveredIDs: models,
	})
}

