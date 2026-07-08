package admin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/skadraneshghn/clever-ai-gate/api/dto"
	"github.com/skadraneshghn/clever-ai-gate/internal/credentials"
	"github.com/skadraneshghn/clever-ai-gate/internal/database"
)

// PoolHandler provides CRUD operations for model routing pools.
type PoolHandler struct {
	db    *pgxpool.Pool
	vault *credentials.Vault
}

// NewPoolHandler creates a new pool handler.
func NewPoolHandler(db *pgxpool.Pool, vault *credentials.Vault) *PoolHandler {
	return &PoolHandler{db: db, vault: vault}
}

// List returns all model routing pools.
//
// When the `limit` query parameter is supplied the endpoint responds with a
// paginated envelope ({data, total, limit, offset}) supporting server-side
// pagination, filtering (`search`) and virtualized rendering.
// When `limit` is omitted the endpoint preserves the legacy behaviour of
// returning the full flat array of pools (backward compatibility).
//
// @Summary      List model pools
// @Description  Returns all model routing pools with credential counts. Supports optional pagination via limit/offset and filtering via search.
// @Tags         Pools
// @Produce      json
// @Security     BearerAuth
// @Param        limit   query  int     false  "Page size (enables paginated envelope when present)"  example(100)
// @Param        offset  query  int     false  "Page offset"                                        example(0)
// @Param        search  query  string  false  "Case-insensitive search across model_pattern/strategy"  example(gpt-4o)
// @Success      200  {array}   dto.PoolResponse
// @Failure      500  {object}  dto.ErrorResponse
// @Router       /api/v1/admin/pools [get]
func (h *PoolHandler) List(c *gin.Context) {
	limitStr := c.Query("limit")

	// Legacy path: no pagination requested -> return the full flat list.
	if limitStr == "" {
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
		return
	}

	// Paginated path.
	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit <= 0 {
		limit = 100
	}
	offset := 0
	if offStr := c.Query("offset"); offStr != "" {
		if v, err := strconv.Atoi(offStr); err == nil && v >= 0 {
			offset = v
		}
	}
	search := c.Query("search")

	pools, total, err := database.ListModelPoolsPaginated(c.Request.Context(), h.db, limit, offset, search)
	if err != nil {
		c.JSON(http.StatusInternalServerError, dto.ErrorResponse{Error: "failed to list pools", Details: err.Error()})
		return
	}

	resp := make([]dto.PoolResponse, len(pools))
	for i, p := range pools {
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
			CredentialCount: p.CredentialCount,
			CreatedAt:       p.CreatedAt,
		}
	}

	c.JSON(http.StatusOK, dto.PaginatedPoolsResponse{
		Data:   resp,
		Total:  total,
		Limit:  limit,
		Offset: offset,
	})
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

// TestCredential runs a live health check probe against a pool member key.
// @Summary      Test credential health
// @Description  Decrypts the key and sends a lightweight ping completions/tags request to the upstream provider
// @Tags         Pools
// @Produce      json
// @Security     BearerAuth
// @Param        id       path      int  true  "Pool ID"
// @Param        cred_id  path      int  true  "Credential ID"
// @Success      200      {object}  dto.SuccessResponse
// @Router       /api/v1/admin/pools/{id}/credentials/{cred_id}/test [post]
func (h *PoolHandler) TestCredential(c *gin.Context) {
	poolID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: "invalid pool ID"})
		return
	}

	credID, err := strconv.Atoi(c.Param("cred_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: "invalid credential ID"})
		return
	}

	// 1. Fetch credential
	cred, err := database.GetCredential(c.Request.Context(), h.db, credID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, dto.ErrorResponse{Error: "failed to fetch credential"})
		return
	}
	if cred == nil || cred.PoolID != poolID {
		c.JSON(http.StatusNotFound, dto.ErrorResponse{Error: "credential not found in this pool"})
		return
	}

	// 2. Fetch pool pattern
	pool, err := database.GetModelPool(c.Request.Context(), h.db, poolID)
	if err != nil || pool == nil {
		c.JSON(http.StatusNotFound, dto.ErrorResponse{Error: "pool not found"})
		return
	}

	// 3. Decrypt key
	apiKey, err := h.vault.Decrypt(cred.EncryptedKey)
	if err != nil {
		c.JSON(http.StatusInternalServerError, dto.ErrorResponse{Error: "failed to decrypt credential key"})
		return
	}

	// 4. Send health test probe
	client := &http.Client{Timeout: 8 * time.Second}
	var req *http.Request
	var probeErr error

	if cred.Provider == "ollama" {
		// Ollama provider: check /api/tags
		url := strings.TrimRight(cred.BaseURL, "/") + "/api/tags"
		req, probeErr = http.NewRequestWithContext(c.Request.Context(), "GET", url, nil)
		if probeErr == nil {
			req.Header.Set("Authorization", "Bearer "+apiKey)
		}
	} else {
		// Custom, NVIDIA, or OpenAI providers: check chat completions
		url := strings.TrimRight(cred.BaseURL, "/")
		if !strings.HasSuffix(url, "/v1") && cred.Provider != "custom" {
			url = url + "/v1"
		}
		url = url + "/chat/completions"

		// Handle wildcards in model name
		testModel := pool.ModelPattern
		if strings.Contains(testModel, "*") {
			// Fallbacks
			if strings.Contains(testModel, "gpt") {
				testModel = "gpt-4o-mini"
			} else if strings.Contains(testModel, "claude") {
				testModel = "claude-3-5-haiku-20241022"
			} else if strings.Contains(testModel, "nvidia") {
				testModel = "nvidia/llama-3.1-nemotron-70b-instruct"
			} else {
				testModel = strings.ReplaceAll(testModel, "*", "latest")
			}
		}
		// Strip provider prefixes for test requests
		testModel = strings.TrimPrefix(testModel, "nvidia/")
		testModel = strings.TrimPrefix(testModel, "ollama/")

		payload := map[string]interface{}{
			"model":      testModel,
			"messages":   []map[string]string{{"role": "user", "content": "ping"}},
			"max_tokens": 1,
		}
		bodyBytes, _ := json.Marshal(payload)
		req, probeErr = http.NewRequestWithContext(c.Request.Context(), "POST", url, bytes.NewReader(bodyBytes))
		if probeErr == nil {
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+apiKey)
		}
	}

	isHealthy := false
	var lastErrorText *string

	if probeErr != nil {
		errStr := fmt.Sprintf("failed to build probe request: %v", probeErr)
		lastErrorText = &errStr
	} else {
		resp, err := client.Do(req)
		if err != nil {
			errStr := fmt.Sprintf("connection failed: %v", err)
			lastErrorText = &errStr
		} else {
			defer resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				isHealthy = true
			} else {
				// Read response body error segment
				limitReader := io.LimitReader(resp.Body, 1024)
				respBytes, _ := io.ReadAll(limitReader)
				errStr := fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(respBytes))
				lastErrorText = &errStr
			}
		}
	}

	// 5. Update DB health state
	err = database.UpdateCredentialHealthState(c.Request.Context(), h.db, credID, isHealthy, lastErrorText)
	if err != nil {
		c.JSON(http.StatusInternalServerError, dto.ErrorResponse{Error: "failed to save health check status", Details: err.Error()})
		return
	}

	// 6. Broadcast reload statement
	_, _ = h.db.Exec(c.Request.Context(), "NOTIFY config_change, 'model_pools:reload'")

	c.JSON(http.StatusOK, gin.H{
		"is_healthy": isHealthy,
		"error":      lastErrorText,
	})
}

// GetLogs returns telemetry logs for a pool, supporting search keyword and vector semantic search.
// @Summary      Get pool logs
// @Description  Queries recent request logs for a pool with support for standard search filters and semantic vector similarity search
// @Tags         Pools
// @Produce      json
// @Security     BearerAuth
// @Param        id              path      int     true   "Pool ID"
// @Param        limit           query     int     false  "Row limit (default 20)"
// @Param        offset          query     int     false  "Row offset"
// @Param        tenant_id       query     string  false  "Tenant API key or ID filter"
// @Param        status          query     string  false  "HTTP status filter ('success' or 'error')"
// @Param        search          query     string  false  "Text keyword search"
// @Param        semantic_query  query     string  false  "Semantic AI vector similarity query"
// @Success      200             {array}   database.LogWithVectorRow
// @Router       /api/v1/admin/pools/{id}/logs [get]
func (h *PoolHandler) GetLogs(c *gin.Context) {
	poolID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: "invalid pool ID"})
		return
	}

	// Fetch pool model pattern
	pool, err := database.GetModelPool(c.Request.Context(), h.db, poolID)
	if err != nil || pool == nil {
		c.JSON(http.StatusNotFound, dto.ErrorResponse{Error: "pool not found"})
		return
	}

	limit := 20
	if l := c.Query("limit"); l != "" {
		if val, err := strconv.Atoi(l); err == nil && val > 0 {
			limit = val
			if limit > 100 {
				limit = 100
			}
		}
	}

	offset := 0
	if o := c.Query("offset"); o != "" {
		if val, err := strconv.Atoi(o); err == nil && val >= 0 {
			offset = val
		}
	}

	tenantFilter := c.Query("tenant_id")
	statusFilter := c.Query("status")
	searchKeyword := c.Query("search")
	semanticQuery := c.Query("semantic_query")

	// Generate query embedding if semantic vector search requested
	var embeddingVector []float32
	if semanticQuery != "" {
		embeddingVector = database.GenerateEmbedding(semanticQuery)
	}

	logs, err := database.ListLogsForPool(
		c.Request.Context(),
		h.db,
		pool.ModelPattern,
		limit,
		offset,
		tenantFilter,
		statusFilter,
		searchKeyword,
		semanticQuery,
		embeddingVector,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, dto.ErrorResponse{Error: "failed to query logs", Details: err.Error()})
		return
	}

	c.JSON(http.StatusOK, logs)
}

// BulkDelete deletes multiple model pools at once.
// @Summary      Bulk delete model pools
// @Description  Permanently removes multiple model pools by their IDs and cascades to their credentials
// @Tags         Pools
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        request   body      dto.BulkDeleteRequest  true  "IDs of model pools to delete"
// @Success      200       {object}  dto.SuccessResponse
// @Failure      400       {object}  dto.ErrorResponse
// @Failure      500       {object}  dto.ErrorResponse
// @Router       /api/v1/admin/pools/bulk-delete [post]
func (h *PoolHandler) BulkDelete(c *gin.Context) {
	var req dto.BulkDeleteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: "invalid request body", Details: err.Error()})
		return
	}

	err := database.DeleteModelPoolsBulk(c.Request.Context(), h.db, req.IDs)
	if err != nil {
		c.JSON(http.StatusInternalServerError, dto.ErrorResponse{Error: "failed to delete pools in bulk", Details: err.Error()})
		return
	}

	c.JSON(http.StatusOK, dto.SuccessResponse{Message: fmt.Sprintf("%d model pools deleted successfully", len(req.IDs))})
}


