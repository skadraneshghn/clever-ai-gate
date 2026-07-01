package admin

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

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

// List returns all provider credentials with masked keys.
// @Summary      List credentials
// @Description  Returns all provider credentials across all pools with masked API keys
// @Tags         Credentials
// @Produce      json
// @Security     BearerAuth
// @Success      200  {array}   dto.CredentialResponse
// @Failure      500  {object}  dto.ErrorResponse
// @Router       /api/v1/admin/credentials [get]
func (h *CredentialHandler) List(c *gin.Context) {
	creds, err := database.ListAllCredentials(c.Request.Context(), h.db)
	if err != nil {
		c.JSON(http.StatusInternalServerError, dto.ErrorResponse{Error: "failed to list credentials", Details: err.Error()})
		return
	}

	resp := make([]dto.CredentialResponse, len(creds))
	for i, cr := range creds {
		resp[i] = dto.CredentialResponse{
			ID:           cr.ID,
			PoolID:       cr.PoolID,
			Provider:     cr.Provider,
			BaseURL:      cr.BaseURL,
			Weight:       cr.Weight,
			IsHealthy:    cr.IsHealthy,
			LastError:    cr.LastError,
			KeyMask:      dto.MaskAPIKey(cr.EncryptedKey),
			ModelPattern: cr.ModelPattern,
			Prefix:       cr.Prefix,
			CreatedAt:    cr.CreatedAt,
		}
	}

	c.JSON(http.StatusOK, resp)
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

	id, err := database.CreateCredential(c.Request.Context(), h.db, req.PoolID, req.Provider, encryptedKey, req.BaseURL, req.Weight, req.Prefix)
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
		Prefix:    req.Prefix,
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
		Prefix:    cred.Prefix,
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

	err = database.UpdateCredential(c.Request.Context(), h.db, id, req.Provider, encryptedKey, req.BaseURL, req.Weight, req.IsHealthy, req.Prefix)
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

	if req.APIKey == "" {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: "api_key is required for NVIDIA provider"})
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

// RegisterOllamaProvider auto-discovers all models available on an Ollama instance,
// creates model pools for each, and binds the credential to all of them in one transaction.
// Multiple Ollama accounts can be registered — identical models are grouped into the same
// pool for automatic load balancing and failover.
// @Summary      Auto-discover Ollama Models
// @Description  Connects to an Ollama instance, hits the /v1/models endpoint, and registers all available models automatically
// @Tags         Credentials
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body  body      dto.DiscoverProviderRequest  true  "Ollama provider details"
// @Success      200   {object}  dto.DiscoverProviderResponse
// @Failure      400   {object}  dto.ErrorResponse
// @Failure      500   {object}  dto.ErrorResponse
// @Router       /api/v1/admin/providers/ollama [post]
func (h *CredentialHandler) RegisterOllamaProvider(c *gin.Context) {
	var req dto.DiscoverProviderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: "invalid request body", Details: err.Error()})
		return
	}

	if req.Provider != "ollama" {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: "invalid provider, must be 'ollama'"})
		return
	}

	if req.Weight <= 0 {
		req.Weight = 1
	}

	// API key is optional for Ollama — local instances typically require no authentication.
	// When provided, it is sent as a Bearer token (useful for Ollama behind an auth proxy).
	count, models, err := credentials.DiscoverAndRegisterOllamaModels(
		c.Request.Context(),
		h.db,
		h.vault,
		req.APIKey,
		req.BaseURL,
		req.Weight,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, dto.ErrorResponse{Error: "Ollama auto-discovery failed", Details: err.Error()})
		return
	}

	c.JSON(http.StatusOK, dto.DiscoverProviderResponse{
		Message:       "Successfully synchronized all Ollama models",
		ModelsCount:   count,
		DiscoveredIDs: models,
	})
}

// RegisterOpenRouterProvider auto-discovers all FREE models available on OpenRouter,
// creates model pools for each, and binds the credential to all of them in one transaction.
// Free-tier models are identified by the `:free` suffix in their ID (OpenRouter's canonical
// free-tier designation) and/or zero prompt/completion pricing.
//
// @Summary      Auto-discover OpenRouter Free Models
// @Description  Connects to OpenRouter, fetches the model catalog, and registers only free-tier models
// @Tags         Credentials
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body  body      dto.DiscoverProviderRequest  true  "OpenRouter provider details (api_key required, base_url ignored)"
// @Success      200   {object}  dto.DiscoverProviderResponse
// @Failure      400   {object}  dto.ErrorResponse
// @Failure      500   {object}  dto.ErrorResponse
// @Router       /api/v1/admin/providers/openrouter [post]
func (h *CredentialHandler) RegisterOpenRouterProvider(c *gin.Context) {
	var req dto.DiscoverProviderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: "invalid request body", Details: err.Error()})
		return
	}

	if req.APIKey == "" {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: "api_key is required for OpenRouter auto-discovery"})
		return
	}

	if req.Weight <= 0 {
		req.Weight = 1
	}

	count, models, err := credentials.DiscoverAndRegisterOpenRouterModels(
		c.Request.Context(),
		h.db,
		h.vault,
		req.APIKey,
		req.Weight,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, dto.ErrorResponse{Error: "OpenRouter free-model discovery failed", Details: err.Error()})
		return
	}

	c.JSON(http.StatusOK, dto.DiscoverProviderResponse{
		Message:       fmt.Sprintf("Successfully synchronized %d free OpenRouter models", count),
		ModelsCount:   count,
		DiscoveredIDs: models,
	})
}

// RegisterCustomProvider handles POST /api/v1/admin/providers/custom
// It connects to any OpenAI-compatible provider, validates the API key,
// discovers all available models, and registers them for load balancing.
//
// @Summary     Register a generic OpenAI-compatible provider
// @Description Auto-discovers models from any OpenAI-compatible endpoint.
// @Tags        providers
// @Accept      json
// @Produce     json
// @Param       body body     dto.DiscoverProviderRequest true "Provider details"
// @Success     200  {object} dto.DiscoverProviderResponse
// @Failure     400  {object} dto.ErrorResponse
// @Failure     500  {object} dto.ErrorResponse
// @Router      /api/v1/admin/providers/custom [post]
func (h *CredentialHandler) RegisterCustomProvider(c *gin.Context) {
	var req dto.DiscoverProviderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: "invalid request body", Details: err.Error()})
		return
	}

	if req.APIKey == "" {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: "api_key is required for OpenAI-compatible provider discovery"})
		return
	}

	if req.Weight <= 0 {
		req.Weight = 1
	}

	// Use the label field as the provider name, falling back to "custom"
	providerLabel := req.Label
	if providerLabel == "" {
		providerLabel = "custom"
	}

	count, models, err := credentials.DiscoverAndRegisterCustomModels(
		c.Request.Context(),
		h.db,
		h.vault,
		req.APIKey,
		req.BaseURL,
		providerLabel,
		req.Weight,
		req.Prefix,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, dto.ErrorResponse{Error: "OpenAI-compatible provider discovery failed", Details: err.Error()})
		return
	}

	c.JSON(http.StatusOK, dto.DiscoverProviderResponse{
		Message:       fmt.Sprintf("Successfully synchronized %d models from %s", count, providerLabel),
		ModelsCount:   count,
		DiscoveredIDs: models,
	})
}

// RegisterOneMinAIProvider auto-discovers all models available on 1min.ai
// across all five modalities (chat, code, image, audio, video), creates model
// pools for each, and binds the credential to all of them in one transaction.
//
// 1min.ai does not expose a /v1/models endpoint, so the full catalog is maintained
// as a static manifest. The API key is validated via a lightweight chat request
// before registration begins. Adding the provider requires only the API key —
// the base URL is hardcoded to https://api.1min.ai.
//
// @Summary      Auto-discover 1min.ai Models
// @Description  Submits a 1min.ai key, validates it, and registers all models across all modalities automatically
// @Tags         Credentials
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body  body      dto.DiscoverProviderRequest  true  "1min.ai provider details (api_key required, base_url ignored)"
// @Success      200   {object}  dto.DiscoverProviderResponse
// @Failure      400   {object}  dto.ErrorResponse
// @Failure      500   {object}  dto.ErrorResponse
// @Router       /api/v1/admin/providers/1minai [post]
func (h *CredentialHandler) RegisterOneMinAIProvider(c *gin.Context) {
	var req dto.DiscoverProviderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: "invalid request body", Details: err.Error()})
		return
	}

	if req.APIKey == "" {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: "api_key is required for 1min.ai auto-discovery"})
		return
	}

	if req.Weight <= 0 {
		req.Weight = 1
	}

	count, models, err := credentials.DiscoverAndRegisterOneMinAIModels(
		c.Request.Context(),
		h.db,
		h.vault,
		req.APIKey,
		req.Weight,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, dto.ErrorResponse{Error: "1min.ai auto-discovery failed", Details: err.Error()})
		return
	}

	c.JSON(http.StatusOK, dto.DiscoverProviderResponse{
		Message:       fmt.Sprintf("Successfully synchronized %d 1min.ai models across all modalities", count),
		ModelsCount:   count,
		DiscoveredIDs: models,
	})
}

// RegisterCloudflareProvider auto-discovers all Cloudflare Workers AI models
// available under the given Account ID, creates model pools for each one,
// and binds the API token credential to all of them in a single atomic transaction.
//
// Credential storage convention (zero schema migration):
//   - The API Token is encrypted and stored in credentials.encrypted_key.
//   - The Account ID is stored in credentials.base_url as "cloudflare:<accountID>".
//     The rewriter parses it at request time with strings.TrimPrefix.
//
// Each model is registered under two pool patterns for maximum client compatibility:
//   - "cloudflare/@cf/meta/llama-3.1-8b-instruct" (explicit prefix form)
//   - "@cf/meta/llama-3.1-8b-instruct" (clean form)
//
// @Summary      Auto-discover Cloudflare Workers AI Models
// @Description  Connects to Cloudflare, fetches all Workers AI models for the account, and registers them
// @Tags         Credentials
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body  body      dto.DiscoverCloudflareRequest  true  "Cloudflare account_id and api_token"
// @Success      200   {object}  dto.DiscoverProviderResponse
// @Failure      400   {object}  dto.ErrorResponse
// @Failure      500   {object}  dto.ErrorResponse
// @Router       /api/v1/admin/providers/cloudflare [post]
func (h *CredentialHandler) RegisterCloudflareProvider(c *gin.Context) {
	var req dto.DiscoverCloudflareRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: "invalid request body", Details: err.Error()})
		return
	}

	if req.AccountID == "" {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: "account_id is required for Cloudflare Workers AI"})
		return
	}

	if req.APIToken == "" {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: "api_token is required for Cloudflare Workers AI"})
		return
	}

	if req.Weight <= 0 {
		req.Weight = 1
	}

	count, models, err := credentials.DiscoverAndRegisterCloudflareModels(
		c.Request.Context(),
		h.db,
		h.vault,
		req.AccountID,
		req.APIToken,
		req.Weight,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, dto.ErrorResponse{
			Error:   "Cloudflare Workers AI auto-discovery failed",
			Details: err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, dto.DiscoverProviderResponse{
		Message:       fmt.Sprintf("Successfully synchronized %d Cloudflare Workers AI models", count),
		ModelsCount:   count,
		DiscoveredIDs: models,
	})
}

// RefreshAllProviders re-runs auto-discovery for every distinct provider key
// already registered in the database. This is the "sync all" action: it reads
// every unique (provider, encrypted_key, base_url, prefix) combination from the
// credentials table, decrypts the key, and calls the appropriate discovery
// function. New clean-alias pools (e.g. "dall-e-3" alongside "1min/dall-e-3")
// are provisioned automatically — no manual re-submission of keys required.
//
// Providers supported for re-discovery:
//   - nvidia    → DiscoverAndRegisterNvidiaModels
//   - ollama    → DiscoverAndRegisterOllamaModels
//   - openrouter → DiscoverAndRegisterOpenRouterModels
//   - 1minai    → DiscoverAndRegisterOneMinAIModels
//   - all others → DiscoverAndRegisterCustomModels (any OpenAI-compatible)
//
// Results are aggregated across all provider accounts and returned as a single
// summary. Per-provider errors are collected but do not abort the overall refresh.
//
// @Summary      Refresh all providers
// @Description  Re-runs auto-discovery for every provider key already in the database, provisioning any missing alias pools
// @Tags         Credentials
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  dto.DiscoverProviderResponse
// @Failure      500  {object}  dto.ErrorResponse
// @Router       /api/v1/admin/providers/refresh [post]
func (h *CredentialHandler) RefreshAllProviders(c *gin.Context) {
	ctx := c.Request.Context()

	// 1. Query distinct provider accounts from the DB.
	//    We group by (provider, encrypted_key, base_url, prefix) so that each
	//    unique key+endpoint combination is re-discovered exactly once, even if
	//    that key is bound to hundreds of model pools.
	type providerAccount struct {
		Provider     string
		EncryptedKey string
		BaseURL      string
		Prefix       string
		Weight       int
	}

	rows, err := h.db.Query(ctx, `
		SELECT DISTINCT ON (provider, encrypted_key, base_url, COALESCE(prefix,''))
		       provider, encrypted_key, base_url, COALESCE(prefix,'') AS prefix,
		       MAX(weight) OVER (PARTITION BY provider, encrypted_key, base_url, COALESCE(prefix,'')) AS weight
		FROM credentials
		ORDER BY provider, encrypted_key, base_url, COALESCE(prefix,'')
	`)
	if err != nil {
		c.JSON(http.StatusInternalServerError, dto.ErrorResponse{
			Error:   "failed to query provider accounts",
			Details: err.Error(),
		})
		return
	}
	defer rows.Close()

	var accounts []providerAccount
	for rows.Next() {
		var acc providerAccount
		if err := rows.Scan(&acc.Provider, &acc.EncryptedKey, &acc.BaseURL, &acc.Prefix, &acc.Weight); err != nil {
			continue
		}
		accounts = append(accounts, acc)
	}
	rows.Close()

	if len(accounts) == 0 {
		c.JSON(http.StatusOK, dto.DiscoverProviderResponse{
			Message:       "No provider accounts found — nothing to refresh",
			ModelsCount:   0,
			DiscoveredIDs: []string{},
		})
		return
	}

	// 2. Re-run discovery for each unique account.
	var allDiscovered []string
	var providerErrors []string

	for _, acc := range accounts {
		// Decrypt the stored key before passing it to the discovery function.
		apiKey, decErr := h.vault.Decrypt(acc.EncryptedKey)
		if decErr != nil {
			providerErrors = append(providerErrors,
				fmt.Sprintf("%s(%s): decrypt error: %v", acc.Provider, acc.BaseURL, decErr))
			continue
		}

		weight := acc.Weight
		if weight <= 0 {
			weight = 1
		}

		var count int
		var discovered []string
		var discErr error

		switch acc.Provider {
		case "nvidia":
			count, discovered, discErr = credentials.DiscoverAndRegisterNvidiaModels(
				ctx, h.db, h.vault, apiKey, acc.BaseURL, weight)

		case "ollama":
			count, discovered, discErr = credentials.DiscoverAndRegisterOllamaModels(
				ctx, h.db, h.vault, apiKey, acc.BaseURL, weight)

		case "openrouter":
			count, discovered, discErr = credentials.DiscoverAndRegisterOpenRouterModels(
				ctx, h.db, h.vault, apiKey, weight)

		case "1minai":
			count, discovered, discErr = credentials.DiscoverAndRegisterOneMinAIModels(
				ctx, h.db, h.vault, apiKey, weight)

		case "cloudflare":
			// Recover the account ID from the stored base_url convention.
			// base_url is stored as "cloudflare:<accountID>" by RegisterCloudflareProvider.
			accountID := strings.TrimPrefix(acc.BaseURL, "cloudflare:")
			count, discovered, discErr = credentials.DiscoverAndRegisterCloudflareModels(
				ctx, h.db, h.vault, accountID, apiKey, weight)

		default:
			// Any OpenAI-compatible provider (openai, anthropic, deepseek, custom, …)
			count, discovered, discErr = credentials.DiscoverAndRegisterCustomModels(
				ctx, h.db, h.vault, apiKey, acc.BaseURL, acc.Provider, weight, acc.Prefix)
		}

		if discErr != nil {
			providerErrors = append(providerErrors,
				fmt.Sprintf("%s(%s): %v", acc.Provider, acc.BaseURL, discErr))
			continue
		}
		_ = count
		allDiscovered = append(allDiscovered, discovered...)
	}

	// 3. Build the response summary.
	msg := fmt.Sprintf("Refreshed %d provider account(s) — %d model pools synchronized",
		len(accounts)-len(providerErrors), len(allDiscovered))
	if len(providerErrors) > 0 {
		msg += fmt.Sprintf(" (%d account(s) failed: see details)", len(providerErrors))
	}

	// Return partial-success (200) even when some accounts failed so the client
	// can surface the successful ones without treating the whole refresh as fatal.
	c.JSON(http.StatusOK, dto.DiscoverProviderResponse{
		Message:       msg,
		ModelsCount:   len(allDiscovered),
		DiscoveredIDs: allDiscovered,
	})
}

// BulkDelete deletes multiple credentials at once.
// @Summary      Bulk delete credentials
// @Description  Permanently removes multiple provider credentials by their IDs
// @Tags         Credentials
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        request   body      dto.BulkDeleteRequest  true  "IDs of credentials to delete"
// @Success      200       {object}  dto.SuccessResponse
// @Failure      400       {object}  dto.ErrorResponse
// @Failure      500       {object}  dto.ErrorResponse
// @Router       /api/v1/admin/credentials/bulk-delete [post]
func (h *CredentialHandler) BulkDelete(c *gin.Context) {
	var req dto.BulkDeleteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: "invalid request body", Details: err.Error()})
		return
	}

	err := database.DeleteCredentialsBulk(c.Request.Context(), h.db, req.IDs)
	if err != nil {
		c.JSON(http.StatusInternalServerError, dto.ErrorResponse{Error: "failed to delete credentials in bulk", Details: err.Error()})
		return
	}

	c.JSON(http.StatusOK, dto.SuccessResponse{Message: fmt.Sprintf("%d credentials deleted successfully", len(req.IDs))})
}

