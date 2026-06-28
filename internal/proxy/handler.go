package proxy

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/buger/jsonparser"
	"github.com/gin-gonic/gin"
	"github.com/skadraneshghn/clever-ai-gate/internal/cache"
	"github.com/skadraneshghn/clever-ai-gate/internal/credentials"
	"go.uber.org/zap"
)

// metadataScanLimit defines the maximum number of bytes to scan for routing
// metadata (model, stream flag) in the request body. Bodies larger than this
// are assumed to contain heavy multi-modal payloads (Base64 images from IDE
// extensions like Cline). Only the leading segment is parsed for routing;
// the full body is piped directly to the upstream without being scanned.
const metadataScanLimit = 256 * 1024 // 256KB

// Handler is the main proxy handler for AI provider requests.
// It sits on the hot-path and is designed for zero heap allocations
// under normal operation using sync.Pool for buffer reuse.
//
// Gap 2 Enhancement: For payloads exceeding 256KB (multi-modal with Base64
// images), only the leading metadata segment is parsed for routing fields.
// The heavy vision byte stream is piped directly to the upstream connection
// without being copied into memory or scanned by jsonparser.
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
	isNvidia   bool   // True when model uses nvidia/ prefix — triggers reasoning injection
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
	//
	// Gap 2 Fix — Bounded Metadata Scanner:
	// When the body exceeds metadataScanLimit (256KB), the payload likely contains
	// heavy Base64 image data from IDE extensions (Cline sending screenshots, etc.).
	// Scanning the entire multi-megabyte body for the "model" and "stream" fields
	// would trigger heap allocations and GC pauses.
	//
	// Instead, we parse only the leading metadata segment. JSON objects place
	// structural keys (model, stream, messages) before binary content payloads,
	// so the routing fields are always in the first few hundred bytes.
	scanSlice := body
	if len(body) > metadataScanLimit {
		scanSlice = body[:metadataScanLimit]
		h.logger.Debug("large payload detected, using bounded metadata scan",
			zap.Int("body_size", len(body)),
			zap.Int("scan_limit", metadataScanLimit),
		)
	}

	modelBytes, _, _, err := jsonparser.Get(scanSlice, "model")
	if err != nil || len(modelBytes) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing or invalid 'model' field"})
		return
	}
	model := string(modelBytes)

	// --- NVIDIA Prefix Detection ---
	// Models prefixed with "nvidia/" are routed through the NVIDIA NIM pipeline.
	// The prefix is stripped for cache lookup but the isNvidia flag triggers
	// reasoning parameter injection before forwarding to the upstream.
	isNvidia := false
	if strings.HasPrefix(model, "nvidia/") {
		isNvidia = true
		// Don't strip the prefix — the model_pattern in the pool already
		// includes "nvidia/" (e.g., pool pattern = "nvidia/nvidia/nemotron-3-super-120b-a12b")
		h.logger.Debug("nvidia model detected",
			zap.String("model", model),
		)
	}

	// Step 3: Detect streaming mode (also bounded to metadata segment)
	isStream, _ := jsonparser.GetBoolean(scanSlice, "stream")

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

	// --- NVIDIA Reasoning Parameter Injection ---
	// When the model is NVIDIA, inject reasoning_budget and chat_template_kwargs
	// into the request body to enable the thinking/reasoning pipeline.
	// This is transparent to the client (Cline/Kilo don't need to know).
	if isNvidia {
		body = injectNvidiaParams(body, scanSlice, h.logger)
	}

	// Step 5-8: Attempt with automatic failover
	// Note: The FULL body (not scanSlice) is forwarded to the upstream provider.
	// Only the metadata extraction was bounded — the binary payload is piped as-is.
	pctx := &proxyContext{
		model:    model,
		isStream: isStream,
		isNvidia: isNvidia,
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
//
// Gap 3 Fix: The streaming path is wrapped in a panic defender to prevent
// transmuxer crashes (malformed provider data, nil pointers, etc.) from
// taking down the entire gateway process on Clever Cloud.
func (h *Handler) forwardRequest(c *gin.Context, pctx *proxyContext) (statusCode int, err error) {
	// Gap 3: Top-level panic defender for the entire forward path.
	// This catches panics from the stream transmuxer, URL rewriter,
	// or any other component in the request pipeline.
	defer func() {
		if r := recover(); r != nil {
			h.logger.Error("recovered from forward request panic",
				zap.Any("panic", r),
				zap.String("model", pctx.model),
				zap.String("provider", pctx.credential.Credential.Provider),
				zap.ByteString("stack", debug.Stack()),
			)
			statusCode = http.StatusInternalServerError
			err = fmt.Errorf("internal panic: %v", r)
		}
	}()

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
		// Set custom headers for developer playground telemetry
		c.Writer.Header().Set("X-Gateway-Provider", cred.Provider)
		c.Writer.Header().Set("X-Gateway-Model-Pattern", pctx.model)

		// Stream mode: pipe SSE chunks through transmuxer
		// ProxyStream has its own internal panic recovery (Gap 1 + Gap 3)
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
	// Add gateway custom headers
	c.Writer.Header().Set("X-Gateway-Provider", cred.Provider)
	c.Writer.Header().Set("X-Gateway-Model-Pattern", pctx.model)

	c.Writer.WriteHeader(resp.StatusCode)
	io.Copy(c.Writer, resp.Body)

	return resp.StatusCode, nil
}

// findPoolByPrefix searches for a pool matching a model name prefix.
// E.g., "gpt-4o-2024-05-13" matches pool "gpt-4o".
// Also handles NVIDIA namespace (e.g., "nvidia/nvidia/nemotron-3-super-120b-a12b").
func (h *Handler) findPoolByPrefix(model string) (interface{}, bool) {
	// Handle NVIDIA slash-separated model names (e.g., "nvidia/nvidia/nemotron-3-super-120b-a12b")
	if strings.HasPrefix(model, "nvidia/") {
		// Try progressively shorter slash-separated prefixes
		slashParts := strings.Split(model, "/")
		for i := len(slashParts) - 1; i >= 1; i-- {
			prefix := strings.Join(slashParts[:i], "/")
			if val, found := h.cache.Get(cache.PoolKey(prefix)); found {
				return val, true
			}
		}
		// Also try just the provider namespace key "nvidia"
		if val, found := h.cache.Get(cache.PoolKey("nvidia")); found {
			return val, true
		}
	}

	// Try progressively shorter dash-separated prefixes
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

// ModelDetail represents model information in OpenAI format.
type ModelDetail struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

// ModelListResponse represents the OpenAI list models response.
type ModelListResponse struct {
	Object string        `json:"object"`
	Data   []ModelDetail `json:"data"`
}

// ListModels returns a list of configured active model pools.
func (h *Handler) ListModels(c *gin.Context) {
	val, found := h.cache.Get("system:active_models")
	var models []string
	if found {
		models = val.([]string)
	}

	data := make([]ModelDetail, len(models))
	now := time.Now().Unix()
	for i, m := range models {
		data[i] = ModelDetail{
			ID:      m,
			Object:  "model",
			Created: now,
			OwnedBy: "clever-ai-gate",
		}
	}

	c.JSON(http.StatusOK, ModelListResponse{
		Object: "list",
		Data:   data,
	})
}

// --- NVIDIA Reasoning Parameter Injection ---

// injectNvidiaParams injects NVIDIA-specific reasoning parameters into the request body.
//
// Parameters injected:
//   - reasoning_budget: Set to match max_tokens (default 4096 if absent)
//   - chat_template_kwargs: {"enable_thinking": true}
//
// This enables NVIDIA models to return structured reasoning content
// that the NvidiaTransmuxer can normalize into reasoning_content deltas.
//
// The injection uses byte-level manipulation to avoid JSON unmarshal/marshal overhead.
// The caller receives a new byte slice — the original body is not modified.
func injectNvidiaParams(body, scanSlice []byte, logger *zap.Logger) []byte {
	// Check if reasoning_budget is already present (avoid double injection)
	if bytes.Contains(scanSlice, []byte(`"reasoning_budget"`)) {
		return body
	}

	// Extract max_tokens for reasoning_budget (default 4096)
	reasoningBudget := 4096
	if maxTokens, err := jsonparser.GetInt(scanSlice, "max_tokens"); err == nil && maxTokens > 0 {
		reasoningBudget = int(maxTokens)
	}

	// Build the injection payload
	injection := []byte(`,"reasoning_budget":` + strconv.Itoa(reasoningBudget) + `,"chat_template_kwargs":{"enable_thinking":true}`)

	// Find the last '}' in the body (the closing brace of the root JSON object)
	lastBrace := bytes.LastIndexByte(body, '}')
	if lastBrace < 0 {
		logger.Warn("nvidia param injection skipped: no closing brace found in body")
		return body
	}

	// Build the new body: everything before '}' + injection + '}'
	newBody := make([]byte, 0, len(body)+len(injection))
	newBody = append(newBody, body[:lastBrace]...)
	newBody = append(newBody, injection...)
	newBody = append(newBody, body[lastBrace:]...)

	logger.Debug("nvidia reasoning params injected",
		zap.Int("reasoning_budget", reasoningBudget),
		zap.Int("original_size", len(body)),
		zap.Int("new_size", len(newBody)),
	)

	return newBody
}
