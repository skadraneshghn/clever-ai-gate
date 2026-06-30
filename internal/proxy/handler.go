package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
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
	requestStart := time.Now()
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

	// --- Ollama Prefix Detection ---
	// Models prefixed with "ollama/" are routed to Ollama instances.
	// The prefix is stripped from the JSON body before forwarding so the
	// upstream Ollama server receives the clean model name (e.g., "llama3:8b").
	isOllama := false
	if strings.HasPrefix(model, "ollama/") {
		isOllama = true
		h.logger.Debug("ollama model detected",
			zap.String("model", model),
		)
	}

	// Step 3: Detect streaming mode (also bounded to metadata segment)
	isStream, _ := jsonparser.GetBoolean(scanSlice, "stream")

	// --- Access log: first thing we know enough to emit a useful Info entry ---
	// This fires for EVERY request and is the primary tool for diagnosing
	// Kilo Code instability: you can see exactly what model/tenant/path was
	// requested and whether it ever reached the retry loop.
	requestID, _ := c.Get("request_id")
	tenantName, _ := c.Get("tenant_name")
	h.logger.Info("proxy request received",
		zap.String("request_id", fmt.Sprintf("%v", requestID)),
		zap.String("tenant", fmt.Sprintf("%v", tenantName)),
		zap.String("model", model),
		zap.String("path", c.Request.URL.Path),
		zap.Bool("stream", isStream),
		zap.Bool("is_nvidia", strings.HasPrefix(model, "nvidia/")),
		zap.Bool("is_ollama", strings.HasPrefix(model, "ollama/")),
		zap.Int("body_bytes", len(body)),
		zap.String("client_ip", c.ClientIP()),
	)

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

	// --- NVIDIA Payload Sanitization ---
	// For NVIDIA-prefixed models two transforms are applied:
	//   1. Strip the "nvidia/" routing prefix from the JSON "model" field so
	//      NVIDIA NIM receives the clean model ID (e.g. "z-ai/glm-5.1").
	//   2. Inject reasoning params ONLY for architectures that support thinking
	//      (Nemotron, DeepSeek-R1, etc.). Standard models like GLM reject these
	//      params with a 500, so we gate them behind supportsNvidiaReasoning.
	if isNvidia {
		// Determine the clean upstream model ID (strip "nvidia/" prefix)
		upstreamModel := strings.TrimPrefix(model, "nvidia/")

		// Fix 2: Conditional reasoning injection — only for supported architectures
		if supportsNvidiaReasoning(upstreamModel) {
			body = injectNvidiaParams(body, scanSlice, h.logger)
		} else {
			h.logger.Debug("skipping reasoning injection for standard non-thinking model",
				zap.String("model", upstreamModel),
			)
		}

		// Fix 1: Strip the routing prefix from the raw JSON bytes.
		// bytes.Replace is a single-pass O(n) scan — no alloc overhead from
		// json.Unmarshal/Marshal and no risk of key reordering.
		// We replace the full quoted token to avoid partial matches.
		oldToken := []byte(`"` + model + `"`)
		newToken := []byte(`"` + upstreamModel + `"`)
		body = bytes.Replace(body, oldToken, newToken, 1)
	}

	// --- Ollama Payload Sanitization ---
	// For Ollama-prefixed models the routing prefix must be stripped from the
	// JSON "model" field so the upstream Ollama server receives the clean model
	// name it expects (e.g., "llama3:8b" instead of "ollama/llama3:8b").
	// Uses the same byte-level replacement as NVIDIA — single-pass O(n) scan.
	if isOllama {
		upstreamModel := strings.TrimPrefix(model, "ollama/")
		oldToken := []byte(`"` + model + `"`)
		newToken := []byte(`"` + upstreamModel + `"`)
		body = bytes.Replace(body, oldToken, newToken, 1)
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

	h.executeWithRetry(c, pctx, requestStart, 3) // Max 3 attempts across different credentials
}

// executeWithRetry attempts the proxy request, failing over to the next
// credential on transient errors (429, 500, 502, 503, 504).
//
// Flaw A fix: forwardRequest no longer writes anything to c.Writer for retryable
// statuses — it returns the error body in-memory. Only the FINAL attempt (or a
// non-retryable outcome) flushes to the wire, so Kilo Code never receives a
// half-written corrupted HTTP response.
//
// Flaw B fix: When all tokens are on cooldown (AcquireActiveToken returns nil),
// we fall back to AcquireLeastPenalizedToken + a short capped sleep instead of
// immediately returning 503. This keeps single-key setups alive through
// transient NVIDIA backend errors without a 10–30 s blackout.
func (h *Handler) executeWithRetry(c *gin.Context, pctx *proxyContext, requestStart time.Time, maxAttempts int) {
	var lastErrBody []byte // in-memory error body from the last failed attempt

	for attempt := 0; attempt < maxAttempts; attempt++ {
		// Lock-free credential acquisition
		result := pctx.pool.AcquireActiveToken()

		if result == nil {
			// Flaw B: All tokens are currently on cooldown.
			// Find the one closest to expiry and sleep until it is ready,
			// capped at 600ms so IDE extensions stay responsive.
			result = pctx.pool.AcquireLeastPenalizedToken()
			if result == nil {
				h.logger.Error("no credentials in pool at all",
					zap.String("model", pctx.model),
					zap.String("tenant_id", pctx.tenantID),
				)
				c.JSON(http.StatusServiceUnavailable, gin.H{
					"error": "all upstream providers are temporarily unavailable",
					"model": pctx.model,
				})
				return
			}
			// Sleep until the soonest token is ready (max 600ms)
			now := time.Now().UnixNano()
			cooldownUntil := atomic.LoadInt64(&result.Credential.CooldownUntil)
			if cooldownUntil > now {
				sleepFor := time.Duration(cooldownUntil - now)
				const maxSleep = 600 * time.Millisecond
				if sleepFor > maxSleep {
					sleepFor = maxSleep
				}
				h.logger.Warn("all tokens cooling down; sleeping until soonest is ready",
					zap.String("model", pctx.model),
					zap.String("provider", result.Credential.Provider),
					zap.Duration("sleep", sleepFor),
					zap.Int("attempt", attempt+1),
				)
				time.Sleep(sleepFor)
			}
		}
		pctx.credential = result

		// Build and execute the upstream request — does NOT write to c.Writer
		// for retryable status codes (Flaw A fix).
		statusCode, upstreamURL, errBody, err := h.forwardRequest(c, pctx)

		if err == nil && !isRetryableStatus(statusCode) {
			// Terminal outcome — success or a non-retryable client error (e.g. 400).
			// forwardRequest has already written the response to c.Writer.
			// Clear the cooldown on the token that just succeeded — it proved itself healthy.
			pctx.pool.ResetCooldown(result.Index)
			if statusCode >= 200 && statusCode < 400 {
				h.logger.Info("proxy request completed",
					zap.String("model", pctx.model),
					zap.String("provider", result.Credential.Provider),
					zap.String("upstream_url", upstreamURL),
					zap.String("tenant_id", pctx.tenantID),
					zap.Bool("stream", pctx.isStream),
					zap.Int("status", statusCode),
					zap.Int("attempt", attempt+1),
					zap.Duration("elapsed", time.Since(requestStart)),
				)
			} else {
				h.logger.Warn("upstream returned non-retryable client error",
					zap.String("model", pctx.model),
					zap.String("provider", result.Credential.Provider),
					zap.String("upstream_url", upstreamURL),
					zap.String("tenant_id", pctx.tenantID),
					zap.Int("status", statusCode),
					zap.Duration("elapsed", time.Since(requestStart)),
				)
			}
			return
		}

		// Retryable failure — penalize, log, and decide whether to loop.
		lastErrBody = errBody
		cooldownDuration := cooldownForStatus(statusCode)

		isSingleKey := pctx.pool.TotalCount == 1

		if statusCode == http.StatusTooManyRequests && isSingleKey {
			// 429 on a single-key pool: the rate-limit window is typically 30–60s.
			// Retrying within the same request is pointless — there is no other
			// key to fall back to and the window will not reset in milliseconds.
			// Penalize with the full duration and break out immediately so the
			// client gets a clean response and Kilo Code can back off.
			result.FromPool.PenalizeToken(result.Index, cooldownDuration)
			requestID, _ := c.Get("request_id")
			h.logger.Warn("rate-limited on single-key pool, aborting retries",
				zap.String("request_id", fmt.Sprintf("%v", requestID)),
				zap.String("model", pctx.model),
				zap.String("provider", result.Credential.Provider),
				zap.String("upstream_url", upstreamURL),
				zap.String("tenant_id", pctx.tenantID),
				zap.Int("status", statusCode),
				zap.Duration("cooldown", cooldownDuration),
				zap.Duration("elapsed", time.Since(requestStart)),
			)
			break // jump to the "all attempts exhausted" response path
		}

		// For transient server errors (500/502/503/504) on a single-key pool,
		// shorten the cooldown so one cold-start doesn't black out the key for
		// 10–30 seconds. A short retry actually makes sense here.
		if isSingleKey {
			cooldownDuration = 300 * time.Millisecond
		}
		result.FromPool.PenalizeToken(result.Index, cooldownDuration)

		requestID, _ := c.Get("request_id")
		h.logger.Warn("upstream request failed, retrying",
			zap.String("request_id", fmt.Sprintf("%v", requestID)),
			zap.String("model", pctx.model),
			zap.String("provider", result.Credential.Provider),
			zap.String("upstream_url", upstreamURL),
			zap.String("tenant_id", pctx.tenantID),
			zap.Int("status", statusCode),
			zap.Int("attempt", attempt+1),
			zap.Int("max_attempts", maxAttempts),
			zap.Duration("cooldown", cooldownDuration),
			zap.Duration("elapsed", time.Since(requestStart)),
			zap.NamedError("transport_err", err),
		)
	}

	// All attempts exhausted. Now — and only now — write the final error to
	// the client. This is the first time c.Writer is touched for a request
	// that only ever received retryable upstream errors.
	h.logger.Error("all retry attempts exhausted",
		zap.String("model", pctx.model),
		zap.String("tenant_id", pctx.tenantID),
		zap.Int("max_attempts", maxAttempts),
		zap.Duration("total_elapsed", time.Since(requestStart)),
	)
	if len(lastErrBody) > 0 {
		c.Data(http.StatusBadGateway, "application/json", lastErrBody)
	} else {
		c.JSON(http.StatusBadGateway, gin.H{
			"error": "all retry attempts exhausted",
			"model": pctx.model,
		})
	}
}

// forwardRequest sends the request to the upstream provider and returns the
// result without writing anything to c.Writer for retryable error statuses.
//
// Return values:
//   - statusCode: the HTTP status from upstream (or 0 on transport error)
//   - upstreamURL: the exact URL called (for logging)
//   - errBody: the upstream error body read into memory for retryable statuses;
//     nil for success paths (those are streamed/written directly to c.Writer)
//   - err: non-nil only for transport-level failures (DNS, TLS, timeout)
//
// Flaw A fix: For retryable statuses (500, 503, 429…) the upstream body is
// captured in errBody and returned WITHOUT touching c.Writer. executeWithRetry
// can then loop to the next credential. Only once all retries are exhausted
// does the caller flush errBody to the client — ensuring HTTP headers are
// written exactly once on a clean wire.
func (h *Handler) forwardRequest(c *gin.Context, pctx *proxyContext) (statusCode int, upstreamURL string, errBody []byte, err error) {
	defer func() {
		if r := recover(); r != nil {
			h.logger.Error("recovered from forward request panic",
				zap.Any("panic", r),
				zap.String("model", pctx.model),
				zap.String("provider", pctx.credential.Credential.Provider),
				zap.String("upstream_url", upstreamURL),
				zap.ByteString("stack", debug.Stack()),
			)
			statusCode = http.StatusInternalServerError
			err = fmt.Errorf("internal panic: %v", r)
		}
	}()

	cred := pctx.credential.Credential
	upstreamURL = h.rewriter.RewriteURL(cred.Provider, cred.BaseURL, c.Request.URL.Path, pctx.model)

	upstreamReq, err := http.NewRequestWithContext(
		c.Request.Context(),
		c.Request.Method,
		upstreamURL,
		bytes.NewReader(pctx.body),
	)
	if err != nil {
		return http.StatusInternalServerError, upstreamURL, nil, err
	}

	h.rewriter.RewriteHeaders(upstreamReq, cred.Provider, cred.APIKey, c.Request.Header)

	resp, err := h.client.Do(upstreamReq)
	if err != nil {
		h.logger.Error("upstream transport error",
			zap.String("model", pctx.model),
			zap.String("provider", cred.Provider),
			zap.String("upstream_url", upstreamURL),
			zap.Error(err),
		)
		return 0, upstreamURL, nil, err
	}
	defer resp.Body.Close()

	// --- Retryable error: capture body in memory, do NOT touch c.Writer ---
	// This is the Flaw A fix. If we wrote headers here and then the caller
	// retried, the second attempt would try to WriteHeader on an already-sent
	// connection, corrupting the HTTP stream and crashing Kilo Code.
	if isRetryableStatus(resp.StatusCode) {
		const maxErrBodyBytes = 4096
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrBodyBytes))
		h.logger.Error("upstream returned retryable error (not flushed to client yet)",
			zap.String("model", pctx.model),
			zap.String("provider", cred.Provider),
			zap.String("upstream_url", upstreamURL),
			zap.Int("status", resp.StatusCode),
			zap.ByteString("upstream_error_body", body),
		)
		return resp.StatusCode, upstreamURL, body, nil
	}

	// --- Success stream path ---
	if pctx.isStream && resp.StatusCode == http.StatusOK {
		c.Writer.Header().Set("X-Gateway-Provider", cred.Provider)
		c.Writer.Header().Set("X-Gateway-Model-Pattern", pctx.model)
		h.stream.ProxyStream(c, resp, cred.Provider)
		return resp.StatusCode, upstreamURL, nil, nil
	}

	// --- Non-retryable error or non-stream success: write directly to client ---
	// This is the only place c.Writer.WriteHeader is called for these paths,
	// so headers are sent exactly once.
	if resp.StatusCode >= 400 {
		// Non-retryable client error (e.g. 400, 401, 403) — log the body for diagnostics
		const maxErrBodyBytes = 4096
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxErrBodyBytes))
		if readErr == nil && len(body) > 0 {
			h.logger.Error("upstream returned non-retryable error",
				zap.String("model", pctx.model),
				zap.String("provider", cred.Provider),
				zap.String("upstream_url", upstreamURL),
				zap.Int("status", resp.StatusCode),
				zap.ByteString("upstream_error_body", body),
			)
			for key, vals := range resp.Header {
				for _, val := range vals {
					c.Writer.Header().Add(key, val)
				}
			}
			c.Writer.Header().Set("X-Gateway-Provider", cred.Provider)
			c.Writer.Header().Set("X-Gateway-Model-Pattern", pctx.model)
			c.Writer.WriteHeader(resp.StatusCode)
			c.Writer.Write(body) //nolint:errcheck
			return resp.StatusCode, upstreamURL, nil, nil
		}
	}

	// Normal success path
	respBody, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return http.StatusInternalServerError, upstreamURL, nil, readErr
	}

	// --- Ollama native response translation (non-streaming) ---
	// Ollama Cloud returns a native JSON body for non-stream requests that must
	// be translated to OpenAI chat completion format before sending to the client.
	if cred.Provider == "ollama" {
		if translated, ok := translateOllamaResponse(respBody); ok {
			respBody = translated
			for key, vals := range resp.Header {
				for _, val := range vals {
					c.Writer.Header().Add(key, val)
				}
			}
			c.Writer.Header().Set("Content-Type", "application/json")
			c.Writer.Header().Set("X-Gateway-Provider", cred.Provider)
			c.Writer.Header().Set("X-Gateway-Model-Pattern", pctx.model)
			c.Writer.WriteHeader(resp.StatusCode)
			c.Writer.Write(respBody) //nolint:errcheck
			return resp.StatusCode, upstreamURL, nil, nil
		}
	}

	for key, vals := range resp.Header {
		for _, val := range vals {
			c.Writer.Header().Add(key, val)
		}
	}
	c.Writer.Header().Set("X-Gateway-Provider", cred.Provider)
	c.Writer.Header().Set("X-Gateway-Model-Pattern", pctx.model)
	c.Writer.WriteHeader(resp.StatusCode)
	c.Writer.Write(respBody) //nolint:errcheck

	return resp.StatusCode, upstreamURL, nil, nil
}

// translateOllamaResponse converts a native Ollama non-streaming response body
// into an OpenAI-compatible chat completion JSON.
//
// Ollama /api/chat (non-stream) returns:
//
//	{"model":"llama4","message":{"role":"assistant","content":"Hello!"},"done":true,...}
//
// Ollama /api/generate (non-stream) returns:
//
//	{"model":"llama4","response":"Hello!","done":true,...}
//
// Both are translated to the OpenAI /v1/chat/completions shape.
// Returns (translated, true) on success; (nil, false) if not a known Ollama shape.
func translateOllamaResponse(data []byte) ([]byte, bool) {
	var content string

	// Try /api/chat shape first
	if msgContent, _, _, err := jsonparser.Get(data, "message", "content"); err == nil {
		content = string(msgContent)
	} else if response, _, _, err := jsonparser.Get(data, "response"); err == nil {
		// /api/generate shape
		content = string(response)
	} else {
		// Not a recognised Ollama native response — let it pass through unchanged
		return nil, false
	}

	model, _ := jsonparser.GetString(data, "model")
	promptTokens, _ := jsonparser.GetInt(data, "prompt_eval_count")
	completionTokens, _ := jsonparser.GetInt(data, "eval_count")

	// Build an OpenAI-compatible chat completion response
	type message struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	type choice struct {
		Index        int     `json:"index"`
		Message      message `json:"message"`
		FinishReason string  `json:"finish_reason"`
	}
	type usage struct {
		PromptTokens     int64 `json:"prompt_tokens"`
		CompletionTokens int64 `json:"completion_tokens"`
		TotalTokens      int64 `json:"total_tokens"`
	}
	type completion struct {
		ID      string   `json:"id"`
		Object  string   `json:"object"`
		Model   string   `json:"model"`
		Choices []choice `json:"choices"`
		Usage   usage    `json:"usage"`
	}

	result := completion{
		ID:     "chatcmpl-gate",
		Object: "chat.completion",
		Model:  model,
		Choices: []choice{
			{
				Index:        0,
				Message:      message{Role: "assistant", Content: content},
				FinishReason: "stop",
			},
		},
		Usage: usage{
			PromptTokens:     promptTokens,
			CompletionTokens: completionTokens,
			TotalTokens:      promptTokens + completionTokens,
		},
	}

	out, err := json.Marshal(result)
	if err != nil {
		return nil, false
	}
	return out, true
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

	// Handle Ollama slash-separated model names (e.g., "ollama/llama3:8b")
	if strings.HasPrefix(model, "ollama/") {
		// Try progressively shorter slash-separated prefixes
		slashParts := strings.Split(model, "/")
		for i := len(slashParts) - 1; i >= 1; i-- {
			prefix := strings.Join(slashParts[:i], "/")
			if val, found := h.cache.Get(cache.PoolKey(prefix)); found {
				return val, true
			}
		}
		// Also try just the provider namespace key "ollama"
		if val, found := h.cache.Get(cache.PoolKey("ollama")); found {
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

// supportsNvidiaReasoning reports whether the upstream model (nvidia/ prefix already
// stripped) supports NVIDIA's reasoning extensions (reasoning_budget,
// chat_template_kwargs.enable_thinking). Standard models like GLM, Llama-base,
// and Mistral reject these params with a 500, so they must be excluded.
func supportsNvidiaReasoning(upstreamModel string) bool {
	lower := strings.ToLower(upstreamModel)
	return strings.Contains(lower, "nemotron") ||
		strings.Contains(lower, "-r1") ||
		strings.Contains(lower, "reasoning") ||
		strings.Contains(lower, "think")
}

