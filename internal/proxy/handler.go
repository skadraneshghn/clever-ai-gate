package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
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
	"github.com/skadraneshghn/clever-ai-gate/internal/cluster"
	"github.com/skadraneshghn/clever-ai-gate/internal/credentials"
	"github.com/skadraneshghn/clever-ai-gate/internal/telemetry"
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
type Handler struct {
	client      *http.Client
	cache       *cache.Store
	pipeline    *telemetry.Pipeline
	broadcaster *cluster.Broadcaster // nil when Redis not configured; safe no-op
	logger      *zap.Logger
	bufPool     sync.Pool
	rewriter    *Rewriter
	stream      *StreamProxy
}

// NewHandler creates the proxy handler with all its dependencies.
// broadcaster may be nil — all Broadcaster methods are nil-safe no-ops.
func NewHandler(client *http.Client, cacheStore *cache.Store, logger *zap.Logger, pipeline *telemetry.Pipeline, broadcaster *cluster.Broadcaster) *Handler {
	h := &Handler{
		client:      client,
		cache:       cacheStore,
		pipeline:    pipeline,
		broadcaster: broadcaster,
		logger:      logger,
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
	model        string
	isStream     bool
	isNvidia     bool // True when model uses nvidia/ prefix — triggers reasoning injection
	isOneMinAI   bool // True when model uses 1min/ prefix — triggers body/response translation
	isCloudflare bool // True when model uses cloudflare/ prefix — triggers prefix stripping
	isSarvam     bool // True when model uses sarvam/ prefix — triggers prefix stripping
	isPuter      bool // True when model uses puter/ prefix — triggers prefix stripping
	body         []byte
	credential   *credentials.AcquireResult
	pool         *credentials.BalancedChannelPool
	tenantID     string
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

	// Step 2: Extract the "model" field for routing.
	//
	// Two code paths depending on Content-Type:
	//   - JSON (default): zero-alloc bounded jsonparser scan
	//   - multipart/form-data: byte-level search for the model form field
	//     (used by /v1/audio/transcriptions, /v1/images/edits, /v1/files, etc.)
	contentType := c.Request.Header.Get("Content-Type")
	isMultipart := strings.HasPrefix(contentType, "multipart/")

	var model string
	var scanSlice []byte
	var isStream bool

	if isMultipart {
		// Multipart model extraction: search the raw body for the form field
		// named "model". In multipart encoding this appears as:
		//   Content-Disposition: form-data; name="model"\r\n\r\nmodel-value\r\n
		// We do a fast byte scan to locate the value without parsing the
		// entire multipart structure (which would require heap allocations).
		model = extractModelFromMultipart(body)
		if model == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "missing 'model' field in multipart form data"})
			return
		}
		// Multipart requests are never streaming (audio, image, file uploads)
		isStream = false
	} else {
		// JSON path: bounded metadata scan (existing zero-alloc logic)
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
		scanSlice = body
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
		model = string(modelBytes)

		// Step 3: Detect streaming mode (also bounded to metadata segment)
		isStream, _ = jsonparser.GetBoolean(scanSlice, "stream")
	}

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

	// --- 1min.ai Prefix Detection ---
	// Models prefixed with "1min/" are routed through the 1min.ai Feature API
	// translation engine. The proxy body translator converts the OpenAI-compatible
	// request into 1min.ai's type/model/promptObject format, and the response
	// translator converts the aiRecord envelope back to OpenAI format.
	isOneMinAI := false
	if strings.HasPrefix(model, "1min/") {
		isOneMinAI = true
		h.logger.Debug("1min.ai model detected",
			zap.String("model", model),
		)
	}

	// --- Cloudflare Workers AI Prefix Detection ---
	// Models prefixed with "cloudflare/" are routed to Cloudflare Workers AI.
	// The prefix is stripped from the JSON body before forwarding so the upstream
	// receives the clean model ID (e.g. "@cf/meta/llama-3.1-8b-instruct").
	isCloudflare := false
	if strings.HasPrefix(model, "cloudflare/") {
		isCloudflare = true
		h.logger.Debug("cloudflare workers ai model detected",
			zap.String("model", model),
		)
	}

	// --- Sarvam AI Prefix Detection ---
	// Models prefixed with "sarvam/" are routed to Sarvam AI. The prefix is
	// stripped from the JSON body before forwarding so the upstream Sarvam API
	// receives the clean model name (e.g. "sarvam-105b"). Sarvam is natively
	// OpenAI-compatible, so no body/response translation or transmuxer is needed.
	isSarvam := false
	if strings.HasPrefix(model, "sarvam/") {
		isSarvam = true
		h.logger.Debug("sarvam ai model detected",
			zap.String("model", model),
		)
	}

	// --- Puter Prefix Detection ---
	isPuter := false
	if strings.HasPrefix(model, "puter/") {
		isPuter = true
		h.logger.Debug("puter model detected",
			zap.String("model", model),
		)
	}

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
		zap.Bool("is_1minai", isOneMinAI),
		zap.Bool("is_cloudflare", isCloudflare),
		zap.Bool("is_sarvam", isSarvam),
		zap.Bool("is_puter", isPuter),
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

	// --- Cloudflare Payload Sanitization ---
	// For Cloudflare-prefixed models the "cloudflare/" routing prefix must be
	// stripped from the JSON "model" field so Cloudflare receives the clean
	// model ID (e.g. "@cf/meta/llama-3.1-8b-instruct" instead of
	// "cloudflare/@cf/meta/llama-3.1-8b-instruct").
	// Uses the same single-pass O(n) byte-level replacement.
	if isCloudflare {
		upstreamModel := strings.TrimPrefix(model, "cloudflare/")
		oldToken := []byte(`"` + model + `"`)
		newToken := []byte(`"` + upstreamModel + `"`)
		body = bytes.Replace(body, oldToken, newToken, 1)
	}

	// --- Sarvam AI Payload Sanitization ---
	// For Sarvam-prefixed models the "sarvam/" routing prefix must be stripped
	// from the JSON "model" field so the upstream Sarvam API receives the clean
	// model name (e.g. "sarvam-105b"). Uses the same single-pass O(n) byte-level
	// replacement as nvidia/ollama/cloudflare.
	//
	// Note: stripping of OpenAI-only request fields that Sarvam's strict schema
	// rejects (stream_options, logprobs, …) is performed in forwardRequest,
	// gated on cred.Provider == "sarvam", so it covers BOTH the prefixed and
	// the clean-alias routing forms.
	if isSarvam {
		upstreamModel := strings.TrimPrefix(model, "sarvam/")
		oldToken := []byte(`"` + model + `"`)
		newToken := []byte(`"` + upstreamModel + `"`)
		body = bytes.Replace(body, oldToken, newToken, 1)
	}

	// --- Puter Payload Sanitization ---
	if isPuter {
		upstreamModel := strings.TrimPrefix(model, "puter/")
		oldToken := []byte(`"` + model + `"`)
		newToken := []byte(`"` + upstreamModel + `"`)
		body = bytes.Replace(body, oldToken, newToken, 1)
	}

	// Step 5-8: Attempt with automatic failover
	// Note: The FULL body (not scanSlice) is forwarded to the upstream provider.
	// Only the metadata extraction was bounded — the binary payload is piped as-is.
	pctx := &proxyContext{
		model:        model,
		isStream:     isStream,
		isNvidia:     isNvidia,
		isOneMinAI:   isOneMinAI,
		isCloudflare: isCloudflare,
		isSarvam:     isSarvam,
		isPuter:      isPuter,
		body:         body,
		pool:         pool,
	}

	// Retrieve tenant ID from context (set by auth middleware)
	if tenantID, exists := c.Get("tenant_id"); exists {
		pctx.tenantID = tenantID.(string)
	}

	// Try every credential in the pool before giving up.
	// Cap at 20 to avoid runaway loops on huge pools.
	maxAttempts := int(pool.TotalCount)
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	if maxAttempts > 20 {
		maxAttempts = 20
	}
	h.executeWithRetry(c, pctx, requestStart, maxAttempts)
}

// attemptRecord captures the outcome of a single credential attempt within
// the retry loop. A slice of these is built during pool exhaustion and used
// to construct a detailed diagnostic summary in the final OpenAI error envelope.
type attemptRecord struct {
	provider   string
	statusCode int
	credID     int
}

// executeWithRetry attempts the proxy request, cycling through ALL credentials
// in the pool before giving up.
//
// Total exhaustion policy — ANY non-2xx response or transport error rotates
// to the next available key. The loop never aborts early for a single error:
//   - 400/401/402/403/404/422: key rejected or wrong tier — long cooldown, rotate.
//   - 429 single-key pool: abort immediately (no other key can help).
//   - 429 multi-key pool: short cooldown, rotate to next key.
//   - 5xx / transport-mapped (502/504) / non-standard codes: moderate cooldown, rotate.
//   - When all exhausted: canonical OpenAI error envelope is returned to client.
func (h *Handler) executeWithRetry(c *gin.Context, pctx *proxyContext, requestStart time.Time, maxAttempts int) {
	var lastErrBody []byte
	var lastStatus int
	var lastProvider string
	var attempts []attemptRecord
	triedIndices := make(map[int]bool) // deduplicate — never retry the same index twice
	triedCount := 0

	// Safety valve: in a high-concurrency surge, AcquireActiveToken may return
	// the same index multiple times as the atomic cursor races with other requests.
	// The triedIndices guard correctly fires a continue, but with a fixed attempt
	// counter that continue would burn a slot without evaluating a new key — causing
	// premature pool exhaustion errors.
	//
	// Fix: loop on triedCount (unique keys tried) not attempt (total iterations).
	// Safety valve maxSpins prevents an infinite spin if all remaining untried keys
	// are penalized by concurrent requests between our iterations.
	maxSpins := maxAttempts*3 + 1
	spins := 0

retryLoop:
	for triedCount < maxAttempts {
		spins++
		if spins > maxSpins {
			// All remaining pool tokens were penalized by concurrent requests
			// before we could acquire them. Exit gracefully.
			h.logger.Warn("retry loop safety valve — all remaining tokens penalized by concurrent requests",
				zap.String("model", pctx.model),
				zap.String("tenant_id", pctx.tenantID),
				zap.Int("unique_tried", triedCount),
				zap.Int("max_attempts", maxAttempts),
				zap.Int("spins", spins),
			)
			break retryLoop
		}

		// Lock-free credential acquisition
		result := pctx.pool.AcquireActiveToken()

		if result == nil {
			// All tokens are currently on cooldown.
			// Pick the soonest-available one and sleep briefly.
			result = pctx.pool.AcquireLeastPenalizedToken()
			if result == nil {
				h.logger.Error("no credentials in pool at all",
					zap.String("model", pctx.model),
					zap.String("tenant_id", pctx.tenantID),
				)
				finalBody := formatOpenAIError(http.StatusServiceUnavailable, nil,
					"no credentials are configured for model: "+pctx.model)
				c.Data(http.StatusServiceUnavailable, "application/json", finalBody)
				return
			}
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
					zap.Int("unique_tried", triedCount),
				)
				time.Sleep(sleepFor)
			}
		}

		// Skip credentials we've already tried in this request
		if triedIndices[result.Index] {
			continue
		}
		triedIndices[result.Index] = true
		triedCount++
		pctx.credential = result

		statusCode, upstreamURL, errBody, err := h.forwardRequest(c, pctx)

		// ── True 2xx success: forwardRequest already wrote to c.Writer ────────
		if err == nil && statusCode >= 200 && statusCode < 300 {
			pctx.pool.ResetCooldown(result.Index)
			h.logger.Info("proxy request completed",
				zap.String("model", pctx.model),
				zap.String("provider", result.Credential.Provider),
				zap.String("upstream_url", upstreamURL),
				zap.String("tenant_id", pctx.tenantID),
				zap.Bool("stream", pctx.isStream),
				zap.Int("status", statusCode),
				zap.Int("unique_tried", triedCount),
				zap.Duration("elapsed", time.Since(requestStart)),
			)

			// Emit success telemetry (zero-alloc pool pattern)
			if h.pipeline != nil {
				promptText := extractPromptText(pctx.body)
				var responseText string
				var completionTokens int
				var promptTokens int

				if pctx.isStream && statusCode == http.StatusOK {
					type streamResult struct {
						Text   string `json:"text"`
						Tokens int    `json:"tokens"`
					}
					var sResult streamResult
					if err := json.Unmarshal(errBody, &sResult); err == nil {
						responseText = sResult.Text
						completionTokens = sResult.Tokens
					}
				} else {
					responseText = extractResponseText(errBody)
					completionTokens = extractTokens(errBody, "completion")
				}

				promptTokens = extractTokens(pctx.body, "prompt")
				if promptTokens == 0 {
					promptTokens = len(promptText) / 4
				}
				if completionTokens == 0 && responseText != "" {
					completionTokens = len(responseText) / 4
				}

				entry := telemetry.AcquireEntry()
				entry.TenantID = pctx.tenantID
				entry.Model = pctx.model
				entry.Provider = result.Credential.Provider
				entry.PromptTokens = promptTokens
				entry.CompletionTokens = completionTokens
				entry.LatencyMs = int(time.Since(requestStart).Milliseconds())
				entry.StatusCode = statusCode
				entry.CreatedAt = time.Now()
				entry.Prompt = promptText
				entry.Response = responseText
				h.pipeline.Emit(entry)
			}
			return
		}

		// ── Non-2xx or unrecovered panic: record, penalize, rotate ────────────
		// forwardRequest maps ALL transport errors to numeric status codes (502/504),
		// so err is non-nil only for recovered panics inside forwardRequest (500).
		recStatus := statusCode
		if err != nil {
			recStatus = http.StatusInternalServerError
		}
		attempts = append(attempts, attemptRecord{
			provider:   result.Credential.Provider,
			statusCode: recStatus,
			credID:     result.Credential.ID,
		})
		lastErrBody = errBody
		lastStatus = recStatus
		lastProvider = result.Credential.Provider
		cooldownDuration := cooldownForStatus(recStatus)
		isSingleKey := pctx.pool.TotalCount == 1

		requestID, _ := c.Get("request_id")

		if isCredentialAuthError(recStatus) {
			// 400/401/402/403/404/422: this key cannot serve this model.
			// Long cooldown + immediately rotate to the next available key.
			result.FromPool.PenalizeToken(result.Index, cooldownDuration)
			h.broadcaster.PublishPenalize(result.FromPool.ModelPattern, result.Credential.ID, result.Index, time.Now().Add(cooldownDuration))
			h.logger.Warn("credential rejected by upstream — rotating to next key",
				zap.String("request_id", fmt.Sprintf("%v", requestID)),
				zap.String("model", pctx.model),
				zap.String("provider", result.Credential.Provider),
				zap.String("upstream_url", upstreamURL),
				zap.Int("status", recStatus),
				zap.Int("credential_id", result.Credential.ID),
				zap.Int("keys_tried", triedCount),
				zap.Duration("cooldown", cooldownDuration),
			)
			continue // immediately rotate
		}

		if recStatus == http.StatusTooManyRequests && isSingleKey {
			// 429 on a single-key pool: abort — no other key to rotate to.
			result.FromPool.PenalizeToken(result.Index, cooldownDuration)
			h.broadcaster.PublishPenalize(result.FromPool.ModelPattern, result.Credential.ID, result.Index, time.Now().Add(cooldownDuration))
			h.logger.Warn("rate-limited on single-key pool — aborting retries",
				zap.String("request_id", fmt.Sprintf("%v", requestID)),
				zap.String("model", pctx.model),
				zap.String("provider", result.Credential.Provider),
				zap.Int("status", recStatus),
				zap.Duration("cooldown", cooldownDuration),
			)
			break retryLoop // labeled break exits the for loop, not just a switch
		}

		// All other non-2xx (429 multi-key, 5xx, non-standard, transport-mapped 502/504):
		// apply cooldown and rotate to the next available key.
		if isSingleKey {
			cooldownDuration = 300 * time.Millisecond
		}
		result.FromPool.PenalizeToken(result.Index, cooldownDuration)
		h.broadcaster.PublishPenalize(result.FromPool.ModelPattern, result.Credential.ID, result.Index, time.Now().Add(cooldownDuration))
		h.logger.Warn("upstream request failed — rotating to next key",
			zap.String("request_id", fmt.Sprintf("%v", requestID)),
			zap.String("model", pctx.model),
			zap.String("provider", result.Credential.Provider),
			zap.String("upstream_url", upstreamURL),
			zap.String("tenant_id", pctx.tenantID),
			zap.Int("status", recStatus),
			zap.Int("spins", spins),
			zap.Int("max_attempts", maxAttempts),
			zap.Int("keys_tried", triedCount),
			zap.Duration("cooldown", cooldownDuration),
			zap.Duration("elapsed", time.Since(requestStart)),
			zap.NamedError("transport_err", err),
		)
	}

	// ── All credentials exhausted ─────────────────────────────────────────────
	h.logger.Error("all pool credentials exhausted",
		zap.String("model", pctx.model),
		zap.String("tenant_id", pctx.tenantID),
		zap.Int("keys_tried", triedCount),
		zap.Int("last_status", lastStatus),
		zap.String("last_provider", lastProvider),
		zap.Duration("total_elapsed", time.Since(requestStart)),
	)

	// Emit exhaustion telemetry (zero-alloc pool pattern)
	if h.pipeline != nil {
		promptText := extractPromptText(pctx.body)
		promptTokens := extractTokens(pctx.body, "prompt")
		if promptTokens == 0 {
			promptTokens = len(promptText) / 4
		}
		entry := telemetry.AcquireEntry()
		entry.TenantID = pctx.tenantID
		entry.Model = pctx.model
		entry.Provider = lastProvider
		entry.PromptTokens = promptTokens
		entry.LatencyMs = int(time.Since(requestStart).Milliseconds())
		entry.StatusCode = http.StatusBadGateway
		entry.ErrorMessage = buildAttemptSummary(pctx.model, attempts)
		entry.CreatedAt = time.Now()
		entry.Prompt = promptText
		h.pipeline.Emit(entry)
	}

	// Never dump raw upstream bytes — always return a canonical OpenAI error envelope.
	summary := buildAttemptSummary(pctx.model, attempts)
	finalBody := formatOpenAIError(lastStatus, lastErrBody, summary)
	c.Data(http.StatusBadGateway, "application/json", finalBody)
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
	modelName := pctx.model
	bodyBytes := pctx.body

	// If the credential has an optional prefix, we must strip it from both the model ID
	// passed to the rewriter and the JSON payload sent upstream.
	if cred.Prefix != "" {
		prefixSlash := cred.Prefix + "/"
		if strings.HasPrefix(modelName, prefixSlash) {
			modelName = strings.TrimPrefix(modelName, prefixSlash)

			// Replace model name inside JSON body bytes.
			// Same O(n) replacement logic as nvidia/ollama.
			oldToken := []byte(`"` + pctx.model + `"`)
			newToken := []byte(`"` + modelName + `"`)
			bodyBytes = bytes.Replace(bodyBytes, oldToken, newToken, 1)
		}
	}

	// --- 1min.ai Request Body Translation ---
	// The 1min.ai API uses a completely different request format
	// (type/model/promptObject). The translator converts the OpenAI-compatible
	// body into the 1min.ai Feature API format. For multipart requests (audio
	// transcription), the audio file is uploaded to the 1min.ai Asset API first.
	var contentTypeOverride string
	if cred.Provider == "1minai" {
		translated, ctOverride, trErr := translateOneMinAIRequest(
			pctx.model, c.Request.URL.Path, bodyBytes,
			c.Request.Header.Get("Content-Type"), cred.APIKey, h.client,
		)
		if trErr != nil {
			h.logger.Error("1min.ai request translation failed",
				zap.String("model", pctx.model),
				zap.Error(trErr),
			)
			return http.StatusInternalServerError, upstreamURL, nil, trErr
		}
		bodyBytes = translated
		contentTypeOverride = ctOverride
	}

	// --- Cloudflare Workers AI Request Body Translation ---
	// Cloudflare's /ai/run/{model} text-to-image endpoint rejects unknown
	// OpenAI fields (model, n, size, response_format) with an "Invalid input"
	// (code 8002) error. We translate image-generation requests down to the
	// bare {"prompt":"..."} body Cloudflare expects. Chat completions are
	// served via Cloudflare's OpenAI-compatible /ai/v1 endpoint and pass
	// through unchanged.
	if cred.Provider == "cloudflare" && isCloudflareImageRequest(c.Request.URL.Path) {
		translated, ctOverride, trErr := translateCloudflareImageRequest(bodyBytes)
		if trErr != nil {
			h.logger.Error("cloudflare request translation failed",
				zap.String("model", pctx.model),
				zap.Error(trErr),
			)
			return http.StatusInternalServerError, upstreamURL, nil, trErr
		}
		bodyBytes = translated
		contentTypeOverride = ctOverride
	}

	// --- Sarvam AI Request Sanitization ---
	// Sarvam's chat-completions schema is a strict subset of OpenAI's. Unknown
	// top-level fields (stream_options, logprobs, service_tier, …) are rejected
	// with HTTP 422, which the gateway would otherwise mis-attribute to a broken
	// key (20–30 min cooldown per credential). Strip them here so any
	// OpenAI-compatible frontend works with Sarvam unchanged. This runs for BOTH
	// routing forms (prefixed "sarvam/…" and clean alias) since it is gated on
	// the resolved credential's provider, not the model prefix. The common
	// clean-request path is allocation-free (fast presence probe).
	if cred.Provider == "sarvam" {
		bodyBytes = sanitizeSarvamRequest(bodyBytes)
	}

	upstreamURL = h.rewriter.RewriteURL(cred.Provider, cred.BaseURL, c.Request.URL.Path, modelName)

	// --- 1min.ai Streaming ---
	// 1min.ai enables streaming via the ?isStreaming=true query parameter
	if cred.Provider == "1minai" && pctx.isStream {
		upstreamURL += "?isStreaming=true"
	}

	upstreamReq, err := http.NewRequestWithContext(
		c.Request.Context(),
		c.Request.Method,
		upstreamURL,
		bytes.NewReader(bodyBytes),
	)
	if err != nil {
		return http.StatusInternalServerError, upstreamURL, nil, err
	}

	h.rewriter.RewriteHeaders(upstreamReq, cred.Provider, cred.APIKey, c.Request.Header)

	// Override Content-Type for 1min.ai (e.g., multipart STT → JSON)
	if contentTypeOverride != "" {
		upstreamReq.Header.Set("Content-Type", contentTypeOverride)
	}

	resp, err := h.client.Do(upstreamReq)
	if err != nil {
		// Map transport-level failures to numeric HTTP status codes so the
		// total-exhaustion retry loop can penalize and rotate to the next key.
		// forwardRequest never returns a non-nil Go error for transport issues;
		// only unrecovered panics (caught by defer/recover above) do.
		mapped := http.StatusBadGateway // connection reset, DNS, TLS handshake
		if isContextTimeoutError(err) {
			mapped = http.StatusGatewayTimeout
		}
		h.logger.Error("upstream transport error — mapped to status for retry",
			zap.String("model", pctx.model),
			zap.String("provider", cred.Provider),
			zap.String("upstream_url", upstreamURL),
			zap.Int("mapped_status", mapped),
			zap.Error(err),
		)
		return mapped, upstreamURL, []byte(err.Error()), nil
	}
	defer resp.Body.Close()

	// ── Total exhaustion policy: capture ALL non-2xx without touching c.Writer ──
	// forwardRequest never writes error responses to the wire. executeWithRetry
	// decides whether to rotate to another key or flush the normalised error.
	// This invariant prevents double WriteHeader corruption on retried connections
	// and enables any error code (400, 404, 422, 444, 599, …) to trigger rotation.
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		const maxErrBodyBytes = 4096
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrBodyBytes))
		h.logger.Error("upstream returned error (buffered for retry evaluation)",
			zap.String("model", pctx.model),
			zap.String("provider", cred.Provider),
			zap.String("upstream_url", upstreamURL),
			zap.Int("status", resp.StatusCode),
			zap.ByteString("upstream_error_body", body),
		)
		return resp.StatusCode, upstreamURL, body, nil
	}

	// --- Success stream path ---
	// --- Success stream path ---
	if pctx.isStream && resp.StatusCode == http.StatusOK {
		c.Writer.Header().Set("X-Gateway-Provider", cred.Provider)
		c.Writer.Header().Set("X-Gateway-Model-Pattern", pctx.model)
		responseText, completionTokens := h.stream.ProxyStream(c, resp, cred.Provider)

		// Pack completion tokens and responseText into a temporary json to pass back to the retry worker
		type streamResult struct {
			Text   string `json:"text"`
			Tokens int    `json:"tokens"`
		}
		resJSON, _ := json.Marshal(streamResult{Text: responseText, Tokens: completionTokens})
		return resp.StatusCode, upstreamURL, resJSON, nil
	}

	// All non-2xx responses are captured by the universal error block above.
	// Only 2xx responses reach this point — safe to write to c.Writer.

	// --- Ollama native response translation (non-streaming) ---
	// Ollama Cloud returns a native JSON body for non-stream requests that must
	// be translated to OpenAI chat completion format before sending to the client.
	// Only Ollama requires buffering — all other providers stream directly below.
	if cred.Provider == "ollama" {
		respBody, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return http.StatusInternalServerError, upstreamURL, nil, readErr
		}
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
			return resp.StatusCode, upstreamURL, respBody, nil
		}
		// Ollama response didn't match native format — fall through to stream as-is
		for key, vals := range resp.Header {
			for _, val := range vals {
				c.Writer.Header().Add(key, val)
			}
		}
		c.Writer.Header().Set("X-Gateway-Provider", cred.Provider)
		c.Writer.Header().Set("X-Gateway-Model-Pattern", pctx.model)
		c.Writer.WriteHeader(resp.StatusCode)
		c.Writer.Write(respBody) //nolint:errcheck
		return resp.StatusCode, upstreamURL, respBody, nil
	}

	// --- 1min.ai Response Translation (non-streaming) ---
	// The 1min.ai API returns responses in an aiRecord envelope that must be
	// translated back to OpenAI-compatible format (chat completions, images, etc.)
	if cred.Provider == "1minai" {
		respBody, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return http.StatusInternalServerError, upstreamURL, nil, readErr
		}

		// Gap 1 Fix: 1min.ai can return HTTP 200 with an internal failure inside
		// the aiRecord envelope (e.g. insufficient credits, model unavailable).
		// Detect this and route through the retry loop so the credential is
		// penalized and failover can occur — same as a real HTTP error.
		if status, _ := jsonparser.GetString(respBody, "aiRecord", "status"); status == "FAILED" {
			errMsg, _ := jsonparser.GetString(respBody, "aiRecord", "aiRecordDetail", "resultObject", "[0]")
			if errMsg == "" {
				errMsg = "1min.ai internal failure"
			}
			h.logger.Warn("1min.ai returned internal failure (HTTP 200, status=FAILED)",
				zap.String("model", pctx.model),
				zap.String("error", errMsg),
				zap.ByteString("response", respBody),
			)
			// Return 502 so executeWithRetry penalizes the key and rotates
			return http.StatusBadGateway, upstreamURL, respBody, nil
		}

		translated, contentType, trErr := translateOneMinAIResponse(pctx.model, respBody)
		if trErr == nil && translated != nil {
			c.Writer.Header().Set("Content-Type", contentType)
			c.Writer.Header().Set("X-Gateway-Provider", cred.Provider)
			c.Writer.Header().Set("X-Gateway-Model-Pattern", pctx.model)
			c.Writer.WriteHeader(resp.StatusCode)
			c.Writer.Write(translated) //nolint:errcheck
			return resp.StatusCode, upstreamURL, translated, nil
		}
		// Translation failed — write original body as fallback
		h.logger.Warn("1min.ai response translation failed, passing through original",
			zap.String("model", pctx.model),
			zap.Error(trErr),
		)
		for key, vals := range resp.Header {
			for _, val := range vals {
				c.Writer.Header().Add(key, val)
			}
		}
		c.Writer.Header().Set("X-Gateway-Provider", cred.Provider)
		c.Writer.Header().Set("X-Gateway-Model-Pattern", pctx.model)
		c.Writer.WriteHeader(resp.StatusCode)
		c.Writer.Write(respBody) //nolint:errcheck
		return resp.StatusCode, upstreamURL, respBody, nil
	}

	// --- Cloudflare Workers AI Image Response Translation ---
	// Cloudflare's /ai/run text-to-image endpoint returns a {success,result}
	// envelope whose result.image holds a base64-encoded image. We translate
	// it to the OpenAI images/generations shape so OpenAI-compatible clients
	// (and this gateway's own chat app) can consume it directly. Chat
	// completions use the OpenAI-compatible /ai/v1 endpoint and pass through.
	if cred.Provider == "cloudflare" && isCloudflareImageRequest(c.Request.URL.Path) {
		respBody, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return http.StatusInternalServerError, upstreamURL, nil, readErr
		}

		// Cloudflare returns HTTP 200 with success=false for some validation
		// failures (e.g. invalid input). Route those through the retry loop so
		// the credential is penalised and a clear error reaches the client.
		if success, err := jsonparser.GetBoolean(respBody, "success"); err == nil && !success {
			h.logger.Warn("cloudflare returned internal failure (HTTP 200, success=false)",
				zap.String("model", pctx.model),
				zap.ByteString("response", respBody),
			)
			return http.StatusBadGateway, upstreamURL, respBody, nil
		}

		translated, contentType, trErr := translateCloudflareImageResponse(respBody)
		if trErr == nil && translated != nil {
			c.Writer.Header().Set("Content-Type", contentType)
			c.Writer.Header().Set("X-Gateway-Provider", cred.Provider)
			c.Writer.Header().Set("X-Gateway-Model-Pattern", pctx.model)
			c.Writer.WriteHeader(resp.StatusCode)
			c.Writer.Write(translated) //nolint:errcheck
			return resp.StatusCode, upstreamURL, translated, nil
		}
		// Translation failed — write original body as fallback
		h.logger.Warn("cloudflare response translation failed, passing through original",
			zap.String("model", pctx.model),
			zap.Error(trErr),
		)
		for key, vals := range resp.Header {
			for _, val := range vals {
				c.Writer.Header().Add(key, val)
			}
		}
		c.Writer.Header().Set("X-Gateway-Provider", cred.Provider)
		c.Writer.Header().Set("X-Gateway-Model-Pattern", pctx.model)
		c.Writer.WriteHeader(resp.StatusCode)
		c.Writer.Write(respBody) //nolint:errcheck
		return resp.StatusCode, upstreamURL, respBody, nil
	}

	// Normal success path — capture text responses for logging history.
	// For JSON (application/json) content types, we read into memory to allow
	// telemetry indexing. For binary formats (image, audio), we stream directly via io.Copy
	// to avoid memory bloat.
	isJSON := strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json")
	var respBody []byte
	if isJSON {
		respBody, _ = io.ReadAll(resp.Body)
	}

	for key, vals := range resp.Header {
		for _, val := range vals {
			c.Writer.Header().Add(key, val)
		}
	}
	c.Writer.Header().Set("X-Gateway-Provider", cred.Provider)
	c.Writer.Header().Set("X-Gateway-Model-Pattern", pctx.model)
	c.Writer.WriteHeader(resp.StatusCode)

	if isJSON {
		c.Writer.Write(respBody)
		return resp.StatusCode, upstreamURL, respBody, nil
	} else {
		io.Copy(c.Writer, resp.Body) //nolint:errcheck
		return resp.StatusCode, upstreamURL, nil, nil
	}
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

	// Try /api/chat shape first.
	// Use GetString (not Get) to avoid double-escaping: Get returns raw JSON bytes
	// where \n is still 2 bytes (`\` + `n`). GetString returns a Go string with
	// actual characters (real newline, >, etc.) which json.Marshal then encodes correctly.
	if msgContent, err := jsonparser.GetString(data, "message", "content"); err == nil {
		content = msgContent
	} else if response, err := jsonparser.GetString(data, "response"); err == nil {
		// /api/generate shape
		content = response
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

	// Handle 1min.ai slash-separated model names (e.g., "1min/gpt-4o", "1min/flux-schnell")
	if strings.HasPrefix(model, "1min/") {
		slashParts := strings.Split(model, "/")
		for i := len(slashParts) - 1; i >= 1; i-- {
			prefix := strings.Join(slashParts[:i], "/")
			if val, found := h.cache.Get(cache.PoolKey(prefix)); found {
				return val, true
			}
		}
		// Also try just the provider namespace key "1min"
		if val, found := h.cache.Get(cache.PoolKey("1min")); found {
			return val, true
		}
	}

	// Handle Cloudflare slash-separated model names
	// e.g. "cloudflare/@cf/meta/llama-3.1-8b-instruct"
	if strings.HasPrefix(model, "cloudflare/") {
		slashParts := strings.Split(model, "/")
		for i := len(slashParts) - 1; i >= 1; i-- {
			prefix := strings.Join(slashParts[:i], "/")
			if val, found := h.cache.Get(cache.PoolKey(prefix)); found {
				return val, true
			}
		}
		// Also try just the provider namespace key "cloudflare"
		if val, found := h.cache.Get(cache.PoolKey("cloudflare")); found {
			return val, true
		}
	}

	// Handle Sarvam AI slash-separated model names (e.g. "sarvam/sarvam-105b")
	if strings.HasPrefix(model, "sarvam/") {
		slashParts := strings.Split(model, "/")
		for i := len(slashParts) - 1; i >= 1; i-- {
			prefix := strings.Join(slashParts[:i], "/")
			if val, found := h.cache.Get(cache.PoolKey(prefix)); found {
				return val, true
			}
		}
		// Also try just the provider namespace key "sarvam"
		if val, found := h.cache.Get(cache.PoolKey("sarvam")); found {
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

// isCredentialAuthError returns true for status codes that indicate the
// specific API key is rejected by the provider, or the model is not accessible
// on this account's plan. All of these warrant an immediate key rotation with
// a long cooldown — the key is broken for this model, not the request itself.
//
//   - 400 Bad Request:          malformed auth header, or model alias not found on account
//   - 401 Unauthorized:         invalid or expired API key
//   - 402 Payment Required:     billing lapsed / quota exhausted
//   - 403 Forbidden:            key tier insufficient for this model
//   - 404 Not Found:            model not accessible under this key's plan
//   - 422 Unprocessable Entity: key accepted but provider rejects this request shape
func isCredentialAuthError(status int) bool {
	switch status {
	case http.StatusBadRequest, // 400
		http.StatusUnauthorized,        // 401
		http.StatusPaymentRequired,     // 402
		http.StatusForbidden,           // 403
		http.StatusNotFound,            // 404
		http.StatusUnprocessableEntity: // 422
		return true
	}
	return false
}

// cooldownForStatus returns the appropriate cooldown duration for a given
// upstream HTTP status code. Covers standard codes, auth anomalies, and
// non-standard codes from load balancers and custom proxies (444, 520, 524, 599…).
func cooldownForStatus(status int) time.Duration {
	switch {
	case status == http.StatusBadRequest ||
		status == http.StatusUnauthorized ||
		status == http.StatusPaymentRequired ||
		status == http.StatusForbidden ||
		status == http.StatusNotFound ||
		status == http.StatusUnprocessableEntity:
		// Auth/access anomalies: the key is broken for this model.
		// 20–30 min jitter prevents thundering-herd re-activation of all bad keys.
		return 20*time.Minute + time.Duration(rand.Intn(int(10*time.Minute)))

	case status == http.StatusTooManyRequests:
		return 30 * time.Second // Rate limited — wait for quota window reset

	case status == http.StatusInternalServerError || status == http.StatusBadGateway:
		return 10 * time.Second // Server error — moderate cooldown

	case status == http.StatusServiceUnavailable:
		return 15 * time.Second // Overloaded — moderate-long cooldown

	case status == http.StatusGatewayTimeout:
		return 5 * time.Second // Timeout — short cooldown, try others first

	default:
		// Non-standard codes (nginx 444, Cloudflare 520/524, custom 599, etc.)
		// 15s safe fallback prevents cascading delays across cluster nodes.
		return 15 * time.Second
	}
}

// ModelDetail represents model information in OpenAI format.
// The Capabilities field is a gateway extension — OpenAI clients ignore
// unknown fields, so this is safe for all compliant tooling.
type ModelDetail struct {
	ID           string          `json:"id"`
	Object       string          `json:"object"`
	Created      int64           `json:"created"`
	OwnedBy      string          `json:"owned_by"`
	Capabilities map[string]bool `json:"capabilities,omitempty"`
}

// ModelListResponse represents the OpenAI list models response.
type ModelListResponse struct {
	Object string        `json:"object"`
	Data   []ModelDetail `json:"data"`
}

// ListModels returns a list of configured active model pools with their
// detected capabilities in OpenAI-compatible format.
func (h *Handler) ListModels(c *gin.Context) {
	val, found := h.cache.Get("system:active_models")

	var data []ModelDetail
	now := time.Now().Unix()

	if found {
		// New enriched format: []credentials.ActiveModel
		if models, ok := val.([]credentials.ActiveModel); ok {
			data = make([]ModelDetail, len(models))
			for i, m := range models {
				data[i] = ModelDetail{
					ID:           m.Pattern,
					Object:       "model",
					Created:      now,
					OwnedBy:      "clever-ai-gate",
					Capabilities: m.Capabilities,
				}
			}
		}
	}

	if data == nil {
		data = []ModelDetail{}
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

// extractModelFromMultipart extracts the "model" field value from a
// multipart/form-data body using a fast byte-level scan.
//
// In multipart encoding, the model field appears as:
//
//	Content-Disposition: form-data; name="model"\r\n\r\nwhisper-1\r\n
//
// We search for the pattern `name="model"` followed by the double CRLF
// separator, then read until the next \r\n or boundary marker.
// This avoids parsing the full multipart structure (no heap allocations
// from mime/multipart) and handles Whisper, DALL-E, and file upload
// requests in sub-microsecond time.
func extractModelFromMultipart(body []byte) string {
	// Look for the field marker
	marker := []byte(`name="model"`)
	idx := bytes.Index(body, marker)
	if idx < 0 {
		return ""
	}

	// Skip past the marker to find the value.
	// After name="model" there may be a CRLF, then optional headers, then
	// a blank CRLF line, then the actual value.
	start := idx + len(marker)
	rest := body[start:]

	// Find the double CRLF (\r\n\r\n) that separates headers from the value
	sep := bytes.Index(rest, []byte("\r\n\r\n"))
	if sep < 0 {
		// Try LF-only variant (some clients use \n\n)
		sep = bytes.Index(rest, []byte("\n\n"))
		if sep < 0 {
			return ""
		}
		rest = rest[sep+2:]
	} else {
		rest = rest[sep+4:]
	}

	// The value ends at the next \r\n (before the boundary)
	end := bytes.Index(rest, []byte("\r\n"))
	if end < 0 {
		end = bytes.Index(rest, []byte("\n"))
		if end < 0 {
			return ""
		}
	}

	value := bytes.TrimSpace(rest[:end])
	return string(value)
}

// --- Telemetry Log Extraction Helpers ---

func extractPromptText(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	// Try "prompt" field first
	if prompt, err := jsonparser.GetString(body, "prompt"); err == nil {
		return prompt
	}
	// Extract the last user message content
	var lastMessage string
	_, _ = jsonparser.ArrayEach(body, func(value []byte, dataType jsonparser.ValueType, offset int, err error) {
		if content, err := jsonparser.GetString(value, "content"); err == nil {
			lastMessage = content
		}
	}, "messages")
	return lastMessage
}

func extractResponseText(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	if content, err := jsonparser.GetString(body, "choices", "[0]", "message", "content"); err == nil {
		return content
	}
	if content, err := jsonparser.GetString(body, "choices", "[0]", "text"); err == nil {
		return content
	}
	return ""
}

func extractTokens(body []byte, promptOrCompletion string) int {
	if val, err := jsonparser.GetInt(body, "usage", promptOrCompletion+"_tokens"); err == nil {
		return int(val)
	}
	return 0
}

// isContextTimeoutError reports whether a transport error is a context timeout
// or deadline exceeded, enabling more precise HTTP status mapping (504 vs 502).
func isContextTimeoutError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "context deadline exceeded") ||
		strings.Contains(msg, "context canceled") ||
		strings.Contains(msg, "timeout")
}

// buildAttemptSummary formats the list of failed credential attempts into a
// human-readable diagnostic string for inclusion in the final error envelope.
func buildAttemptSummary(model string, attempts []attemptRecord) string {
	if len(attempts) == 0 {
		return fmt.Sprintf("all upstream credentials for model %q were exhausted with no successful response", model)
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "all %d credential(s) exhausted for model %q. Attempts: [", len(attempts), model)
	for i, a := range attempts {
		if i > 0 {
			sb.WriteString(", ")
		}
		fmt.Fprintf(&sb, "cred#%d(%s)→%d", a.credID, a.provider, a.statusCode)
	}
	sb.WriteString("]")
	return sb.String()
}

// formatOpenAIError returns a canonical OpenAI error envelope for any failure
// context. It is the single exit point for all error responses sent to clients
// — never raw upstream bytes, never unformatted text or HTML.
//
// Detection priority:
//  1. rawBody is already a valid OpenAI error envelope → return as-is
//     (preserves provider transparency: upstream Claude/OpenAI error details)
//  2. rawBody is JSON with a "message", "error.message", or "detail" field
//     → extract and re-wrap into the canonical schema
//  3. HTML, plain-text, or empty body → sanitize and embed in the message field
func formatOpenAIError(statusCode int, rawBody []byte, summary string) []byte {
	type innerError struct {
		Message string  `json:"message"`
		Type    string  `json:"type"`
		Param   *string `json:"param"`
		Code    string  `json:"code"`
	}
	type envelope struct {
		Error innerError `json:"error"`
	}

	// 1. Already a valid OpenAI error envelope? Return unchanged.
	if len(rawBody) > 0 {
		if errVal, dataType, _, parseErr := jsonparser.Get(rawBody, "error"); parseErr == nil &&
			dataType == jsonparser.Object && len(errVal) > 0 {
			if _, _, _, msgErr := jsonparser.Get(errVal, "message"); msgErr == nil {
				return rawBody // perfect OpenAI shape — preserve provider details
			}
		}
	}

	// 2. Try to extract any upstream message text
	var upstreamMsg string
	if len(rawBody) > 0 {
		if msg, err := jsonparser.GetString(rawBody, "message"); err == nil && msg != "" {
			upstreamMsg = msg
		}
		if upstreamMsg == "" {
			if msg, err := jsonparser.GetString(rawBody, "error", "message"); err == nil && msg != "" {
				upstreamMsg = msg
			}
		}
		if upstreamMsg == "" {
			// FastAPI / Python upstream pattern
			if msg, err := jsonparser.GetString(rawBody, "detail"); err == nil && msg != "" {
				upstreamMsg = msg
			}
		}
	}

	// 3. Build the canonical message
	message := summary
	if upstreamMsg != "" {
		message = summary + ". Last upstream message: " + upstreamMsg
	} else if len(rawBody) > 0 {
		raw := string(rawBody)
		if strings.Contains(raw, "<html") || strings.Contains(raw, "<!DOCTYPE") {
			raw = "[HTML response from upstream load balancer or CDN]"
		} else if len(raw) > 512 {
			raw = raw[:512] + "…"
		}
		if isPrintable(raw) {
			message = summary + ". Last upstream body: " + raw
		}
	}

	out, _ := json.Marshal(envelope{
		Error: innerError{
			Message: message,
			Type:    "gateway_exhaustion_error",
			Param:   nil,
			Code:    "all_providers_failed",
		},
	})
	return out
}

// isPrintable returns true if s consists only of printable ASCII/UTF-8 text.
// Used to filter out binary garbage from upstream responses before embedding
// them in human-readable error messages.
func isPrintable(s string) bool {
	for _, r := range s {
		if r < 0x20 && r != '\n' && r != '\r' && r != '\t' {
			return false
		}
	}
	return true
}
