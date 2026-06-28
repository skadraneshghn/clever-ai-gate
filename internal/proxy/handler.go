package proxy

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/buger/jsonparser"
	"github.com/gin-gonic/gin"
	"github.com/skadraneshghn/clever-ai-gate/internal/cache"
	"github.com/skadraneshghn/clever-ai-gate/internal/credentials"
	"go.uber.org/zap"
)

// Handler is the main proxy handler for AI provider requests.
// It sits on the hot-path and is designed for zero heap allocations
// under normal operation using sync.Pool for buffer reuse.
type Handler struct {
	client      *http.Client
	cache       *cache.Store
	logger      *zap.Logger
	bufPool     sync.Pool
	rewriter    *Rewriter
	stream      *StreamProxy
}

// NewHandler creates the proxy handler with all its dependencies.
func NewHandler(client *http.Client, cacheStore *cache.Store, logger *zap.Logger) *Handler {
	h := &Handler{
		client: client,
		cache:  cacheStore,
		logger: logger,
		bufPool: sync.Pool{
			New: func() interface{} {
				return bytes.NewBuffer(make([]byte, 0, 32*1024)) // 32KB initial scratch
			},
		},
		rewriter: NewRewriter(),
	}
	h.stream = NewStreamProxy(client, logger)
	return h
}

// proxyContext carries extracted fields through the hot-path without allocations.
type proxyContext struct {
	model      string
	isStream   bool
	body       []byte
	credential *credentials.AcquireResult
	pool       *credentials.BalancedChannelPool
	tenantID   string
}

// Handle processes incoming AI requests on the hot-path.
//
// Hot-path sequence (zero database calls, zero mutex locks):
//  1. Read body into sync.Pool scratch buffer
//  2. jsonparser.Get — extract model identifier (zero alloc)
//  3. jsonparser.GetBoolean — detect streaming mode
//  4. Cache lookup: model → BalancedChannelPool
//  5. AcquireActiveToken — lock-free atomic credential selection
//  6. Rewrite request headers + URL for target provider
//  7. Forward to upstream (stream or direct)
//  8. On failure: penalize credential, retry with next
//
// @Summary      Proxy AI request
// @Description  Routes AI requests to upstream providers with automatic failover
// @Tags         Proxy
// @Accept       json
// @Produce      json
// @Param        Authorization  header  string  true  "Bearer virtual-api-key"
// @Param        body           body    object  true  "OpenAI-compatible request body"
// @Router       /v1/{path} [post]
func (h *Handler) Handle(c *gin.Context) {
	// Step 1: Read body into pooled buffer — zero heap allocation
	buf := h.bufPool.Get().(*bytes.Buffer)
	defer func() {
		buf.Reset()
		h.bufPool.Put(buf)
	}()

	if _, err := io.Copy(buf, c.Request.Body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read request body"})
		return
	}
	body := buf.Bytes()

	// Step 2: Zero-alloc field extraction via jsonparser
	modelBytes, _, _, err := jsonparser.Get(body, "model")
	if err != nil || len(modelBytes) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing or invalid 'model' field"})
		return
	}
	model := string(modelBytes)

	// Step 3: Detect streaming mode
	isStream, _ := jsonparser.GetBoolean(body, "stream")

	// Step 4: Cache lookup for routing pool — ~200ns via Ristretto
	poolVal, found := h.cache.Get(cache.PoolKey(model))
	if !found {
		// Try wildcard/prefix matching for model families
		poolVal, found = h.findPoolByPrefix(model)
		if !found {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "no routing pool configured for model: " + model,
			})
			return
		}
	}
	pool := poolVal.(*credentials.BalancedChannelPool)

	// Step 5-8: Attempt with automatic failover
	pctx := &proxyContext{
		model:    model,
		isStream: isStream,
		body:     body,
		pool:     pool,
	}

	// Retrieve tenant ID from context (set by auth middleware)
	if tenantID, exists := c.Get("tenant_id"); exists {
		pctx.tenantID = tenantID.(string)
	}

	h.executeWithRetry(c, pctx, 3) // Max 3 attempts across different credentials
}

// executeWithRetry attempts the proxy request, failing over to the next
// credential on transient errors (429, 500, 502, 503).
func (h *Handler) executeWithRetry(c *gin.Context, pctx *proxyContext, maxAttempts int) {
	for attempt := 0; attempt < maxAttempts; attempt++ {
		// Step 5: Lock-free credential acquisition
		result := pctx.pool.AcquireActiveToken()
		if result == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"error": "all upstream providers are temporarily unavailable",
				"model": pctx.model,
			})
			return
		}
		pctx.credential = result

		// Step 6-7: Build and execute the upstream request
		statusCode, err := h.forwardRequest(c, pctx)

		if err == nil && !isRetryableStatus(statusCode) {
			return // Success or non-retryable client error
		}

		// Step 8: Penalize failed credential and retry
		cooldownDuration := cooldownForStatus(statusCode)
		result.FromPool.PenalizeToken(result.Index, cooldownDuration)

		h.logger.Warn("upstream request failed, retrying",
			zap.String("model", pctx.model),
			zap.String("provider", result.Credential.Provider),
			zap.Int("status", statusCode),
			zap.Int("attempt", attempt+1),
			zap.Duration("cooldown", cooldownDuration),
		)
	}

	// All attempts exhausted
	c.JSON(http.StatusBadGateway, gin.H{
		"error": "all retry attempts exhausted",
		"model": pctx.model,
	})
}

// forwardRequest sends the request to the upstream provider and proxies the response.
func (h *Handler) forwardRequest(c *gin.Context, pctx *proxyContext) (int, error) {
	cred := pctx.credential.Credential

	// Build upstream request
	targetURL := h.rewriter.RewriteURL(cred.Provider, cred.BaseURL, c.Request.URL.Path, pctx.model)

	upstreamReq, err := http.NewRequestWithContext(
		c.Request.Context(),
		c.Request.Method,
		targetURL,
		bytes.NewReader(pctx.body),
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to build upstream request"})
		return 500, err
	}

	// Rewrite headers for the target provider
	h.rewriter.RewriteHeaders(upstreamReq, cred.Provider, cred.APIKey, c.Request.Header)

	// Execute upstream request
	resp, err := h.client.Do(upstreamReq)
	if err != nil {
		return 0, err
	}

	if pctx.isStream && resp.StatusCode == http.StatusOK {
		// Stream mode: pipe SSE chunks through transmuxer
		h.stream.ProxyStream(c, resp, cred.Provider)
		return resp.StatusCode, nil
	}

	// Non-stream mode: read full response and forward
	defer resp.Body.Close()

	// Copy response headers
	for key, vals := range resp.Header {
		for _, val := range vals {
			c.Writer.Header().Add(key, val)
		}
	}
	c.Writer.WriteHeader(resp.StatusCode)
	io.Copy(c.Writer, resp.Body)

	return resp.StatusCode, nil
}

// findPoolByPrefix searches for a pool matching a model name prefix.
// E.g., "gpt-4o-2024-05-13" matches pool "gpt-4o".
func (h *Handler) findPoolByPrefix(model string) (interface{}, bool) {
	// Try progressively shorter prefixes
	parts := strings.Split(model, "-")
	for i := len(parts) - 1; i >= 1; i-- {
		prefix := strings.Join(parts[:i], "-")
		if val, found := h.cache.Get(cache.PoolKey(prefix)); found {
			return val, true
		}
	}
	// Try provider-specific patterns (e.g., "gemini-" prefix)
	for _, prefix := range []string{"gpt-", "claude-", "gemini-", "deepseek-"} {
		if strings.HasPrefix(model, prefix) {
			baseModel := strings.TrimSuffix(prefix, "-")
			if val, found := h.cache.Get(cache.PoolKey(baseModel)); found {
				return val, true
			}
		}
	}
	return nil, false
}

// isRetryableStatus returns true for HTTP status codes that warrant a retry
// with a different credential.
func isRetryableStatus(status int) bool {
	switch status {
	case http.StatusTooManyRequests,       // 429 — rate limited
		http.StatusInternalServerError,    // 500 — server error
		http.StatusBadGateway,             // 502 — bad gateway
		http.StatusServiceUnavailable,     // 503 — overloaded
		http.StatusGatewayTimeout:         // 504 — timeout
		return true
	}
	return false
}

// cooldownForStatus returns appropriate cooldown duration based on error type.
func cooldownForStatus(status int) time.Duration {
	switch status {
	case http.StatusTooManyRequests:
		return 30 * time.Second // Rate limited — longer cooldown
	case http.StatusInternalServerError, http.StatusBadGateway:
		return 10 * time.Second // Server errors — moderate cooldown
	case http.StatusServiceUnavailable:
		return 15 * time.Second // Overloaded — moderate-long cooldown
	case http.StatusGatewayTimeout:
		return 5 * time.Second // Timeout — short cooldown
	default:
		return 5 * time.Second
	}
}
