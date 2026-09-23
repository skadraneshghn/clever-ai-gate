package proxy

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/buger/jsonparser"
	"github.com/gin-gonic/gin"
	"github.com/skadraneshghn/clever-ai-gate/internal/transmux"
	"go.uber.org/zap"
)

// StreamProxy handles SSE (Server-Sent Events) streaming from upstream providers.
// It reads upstream chunks line-by-line, passes them through a provider-specific
// transmuxer for format normalization, and flushes each chunk to the client immediately.
type StreamProxy struct {
	client      *http.Client
	logger      *zap.Logger
	scannerPool sync.Pool
}

// NewStreamProxy creates a new stream proxy handler.
func NewStreamProxy(client *http.Client, logger *zap.Logger) *StreamProxy {
	return &StreamProxy{
		client: client,
		logger: logger,
		scannerPool: sync.Pool{
			New: func() interface{} {
				return make([]byte, 0, 64*1024) // 64KB scanner buffer
			},
		},
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Stream liveness knobs
//
// Production incident (2026-09-23, 52xueai/claude-opus-5): the model streamed
// its opening answer, then went silent for ~109s while composing a large code
// block. The gateway had nothing to relay, so every intermediary between the
// client and the gateway (nginx / Cloudflare / hosting load balancer, idle
// timeouts of ~60-110s) tore the client connection down — surfacing to the
// end user as "unexpected EOF (incomplete chunked read)".
//
// Two mechanisms below fix that class of failure:
//   1. an SSE heartbeat (`: keepalive` comment) emitted while the upstream is
//      silent, which resets every intermediary's idle timer;
//   2. a stall watchdog that gives up on a totally-silent upstream and
//      terminates the client stream cleanly with a visible error event.
//
// Package-level vars (not consts) so tests can shrink them without waiting
// wall-clock minutes.
var (
	// sseKeepaliveInterval is how often the heartbeat fires while the
	// upstream is silent.
	sseKeepaliveInterval = 15 * time.Second

	// streamStallTimeout is the maximum upstream silence tolerated mid-stream
	// before the credential is abandoned. Generous by design: even long
	// Claude-style thinking gaps stream keepalive deltas and never approach
	// this window.
	streamStallTimeout = 5 * time.Minute
)

// sseWriter serializes writes to the client response between the main relay
// loop and the keepalive heartbeat goroutine. SSE comments (`: …`) are legal
// between events per the SSE spec, so interleaving heartbeats with data
// events is protocol-safe. The mutex is released via defer inside every
// method, so a panic mid-write can never leave it locked for the recovery
// path.
type sseWriter struct {
	w  gin.ResponseWriter
	mu sync.Mutex
}

// writeDataEvent emits one `data: <payload>\n\n` SSE event and flushes it.
func (s *sseWriter) writeDataEvent(payload []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.w.Write([]byte("data: ")); err != nil {
		return err
	}
	if _, err := s.w.Write(payload); err != nil {
		return err
	}
	if _, err := s.w.Write([]byte("\n\n")); err != nil {
		return err
	}
	s.w.Flush()
	return nil
}

// writeComment emits one SSE comment line (`: <comment>\n\n`) and flushes it.
// Comments are ignored by every SSE parser — the standard heartbeat channel.
func (s *sseWriter) writeComment(comment string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.w.Write([]byte(": " + comment + "\n\n")); err != nil {
		return err
	}
	s.w.Flush()
	return nil
}

// signalProgressChan notifies the heartbeat goroutine that upstream data was
// just relayed, resetting the stall watchdog. Non-blocking: a dropped signal
// only delays the watchdog by one heartbeat tick.
func signalProgressChan(progress chan struct{}) {
	select {
	case progress <- struct{}{}:
	default:
	}
}

// emitStreamInterrupted terminates an SSE stream with a visible, OpenAI-style
// error event followed by [DONE] so clients render a clear "stream
// interrupted" error instead of a silently truncated (or hanging) response.
// The client already holds partial content at this point — a retry is
// impossible, so the honest, graceful end is an explicit error signal.
func emitStreamInterrupted(sw *sseWriter, cause string) {
	if sw == nil {
		return
	}
	payload := fmt.Sprintf(
		`{"error":{"message":"Upstream connection interrupted before the response completed (%s). Partial content may be missing — retry the request.","type":"server_error","code":"stream_interrupted"}}`,
		sanitizeSSEJSONString(cause, 200),
	)
	_ = sw.writeDataEvent([]byte(payload))
	_ = sw.writeDataEvent([]byte("[DONE]"))
}

// sanitizeSSEJSONString makes an arbitrary error string safe to embed inside a
// JSON string literal (strips quotes, backslashes and control characters).
func sanitizeSSEJSONString(s string, maxLen int) string {
	var b strings.Builder
	for _, r := range s {
		if r == '"' || r == '\\' || r < 0x20 {
			continue
		}
		b.WriteRune(r)
		if b.Len() >= maxLen {
			break
		}
	}
	return b.String()
}

// ProxyStream pipes SSE chunks from upstream to client with format translation,
// and returns the fully accumulated response text along with estimated completion tokens.
func (sp *StreamProxy) ProxyStream(c *gin.Context, upstream *http.Response, provider string, requestedModel string) (responseText string, completionTokens int) {
	// Acquire pooled scanner buffer
	scanBuf := sp.scannerPool.Get().([]byte)

	var responseBuilder strings.Builder
	var tokenEstimate int
	streamStart := time.Now()

	var sw *sseWriter // created once the flusher is confirmed; nil-safe everywhere

	defer func() {
		// Always return the scanner buffer to the pool
		sp.scannerPool.Put(scanBuf[:0])

		// Always close the upstream response body
		upstream.Body.Close()

		// Catch any panic from the transmuxer or write path
		if r := recover(); r != nil {
			sp.logger.Error("recovered from stream processing panic",
				zap.Any("panic", r),
				zap.String("provider", provider),
				zap.ByteString("stack", debug.Stack()),
			)
			// Attempt to signal stream termination to client if connection is still alive
			if sw != nil {
				_ = sw.writeDataEvent([]byte("[DONE]"))
			}
		}
	}()

	// Step 1: Set SSE headers for streaming
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("Transfer-Encoding", "chunked")
	c.Writer.Header().Set("X-Accel-Buffering", "no") // Disable nginx buffering
	c.Writer.WriteHeader(http.StatusOK)

	// Step 2: Create the appropriate transmuxer for this provider
	tmx := transmux.NewTransmuxer(provider)
	defer tmx.Close()

	// Step 3: Create scanner with pooled buffer
	scanner := bufio.NewScanner(upstream.Body)
	scanner.Buffer(scanBuf, 1024*1024) // Max 1MB line (for base64 images)

	if _, ok := c.Writer.(http.Flusher); !ok {
		sp.logger.Error("response writer does not support flushing")
		return "", 0
	}
	sw = &sseWriter{w: c.Writer}

	// Step 3.5: keepalive heartbeat + stall watchdog.
	//
	// Long generations regularly go silent for minutes while the model
	// composes a large code block. Intermediaries between the client and the
	// gateway (nginx, Cloudflare, hosting load balancers) kill "idle"
	// connections after ~60-110s. The heartbeat goroutine below:
	//
	//   1. emits `: keepalive` SSE comments while the upstream is silent,
	//      keeping every intermediary's idle timer reset;
	//   2. aborts the upstream read once the client connection is gone
	//      (heartbeat write fails) so the relay loop exits promptly;
	//   3. aborts the upstream read after streamStallTimeout of total
	//      silence — a truly dead upstream — so the client receives a clean
	//      error event + [DONE] instead of hanging forever.
	kaDone := make(chan struct{})
	progress := make(chan struct{}, 1)
	var clientGone int32   // atomic: heartbeat write failed → client disconnected
	var stallAborted int32 // atomic: watchdog closed the upstream body

	go func() {
		ticker := time.NewTicker(sseKeepaliveInterval)
		defer ticker.Stop()
		lastData := time.Now()
		for {
			select {
			case <-kaDone:
				return
			case <-progress:
				lastData = time.Now()
			case <-ticker.C:
				if err := sw.writeComment("keepalive"); err != nil {
					// Client connection is gone. Unblock the relay loop by
					// closing the upstream body — its next Read errors out.
					atomic.StoreInt32(&clientGone, 1)
					upstream.Body.Close()
					return
				}
				if time.Since(lastData) >= streamStallTimeout {
					atomic.StoreInt32(&stallAborted, 1)
					upstream.Body.Close()
					return
				}
			}
		}
	}()
	defer close(kaDone)

	// Step 4: Read and transmux each SSE line
	var sseEventType string
	var sawToolCalls bool    // tracks if any translated chunk contained tool_calls
	var sawFinishReason bool // tracks if a non-null finish_reason was already sent
	for scanner.Scan() {
		signalProgressChan(progress)
		line := scanner.Bytes()

		// Skip empty lines (SSE delimiter)
		if len(line) == 0 {
			continue
		}

		// Handle SSE data lines
		if bytes.HasPrefix(line, []byte("data: ")) {
			data := line[6:] // Strip "data: " prefix

			// Check for stream termination
			if bytes.Equal(data, []byte("[DONE]")) {
				_ = sw.writeDataEvent([]byte("[DONE]"))
				responseText = responseBuilder.String()
				completionTokens = tokenEstimate
				return
			}

			// Transmux the chunk to OpenAI format
			translated, err := tmx.TranslateChunk(data)
			if err != nil {
				sp.logger.Debug("transmux error, forwarding raw",
					zap.String("provider", provider),
					zap.Error(err),
				)
				translated = data
			}

			// Debug: log raw agentrouter SSE events to diagnose GPT tool call issues
			if provider == "agentrouter" && len(data) > 0 && sseEventType != "ping" {
				blockType, _, _, _ := jsonparser.Get(data, "content_block", "type")
				hasToolCalls := bytes.Contains(translated, []byte(`"tool_calls"`))
				logFields := []zap.Field{
					zap.String("event", sseEventType),
					zap.ByteString("block_type", blockType),
					zap.Bool("has_tool_calls", hasToolCalls),
					zap.Int("raw_len", len(data)),
					zap.Int("translated_len", len(translated)),
				}
				// For content_block_start and input_json_delta, log raw data
				if sseEventType == "content_block_start" || (hasToolCalls && sseEventType == "content_block_delta") {
					truncData := data
					if len(truncData) > 500 {
						truncData = truncData[:500]
					}
					logFields = append(logFields, zap.ByteString("raw_data", truncData))
				}
				sp.logger.Info("agentrouter SSE event", logFields...)
			}

			// Track tool calls and finish_reason for synthetic finish injection
			if bytes.Contains(translated, []byte(`"tool_calls"`)) {
				sawToolCalls = true
			}
			if bytes.Contains(translated, []byte(`"finish_reason":"`)) {
				sawFinishReason = true
			}

			if len(translated) > 0 {
				if requestedModel != "" {
					if _, err := jsonparser.GetString(translated, "model"); err == nil {
						if updated, err := jsonparser.Set(translated, []byte(`"`+requestedModel+`"`), "model"); err == nil {
							translated = updated
						}
					}
				}

				// Capture both content and reasoning_content deltas so the
				// accumulated response text and token estimate include the
				// model's thinking process, not just the final answer. A
				// single OpenAI delta carries one or the other, never both.
				if content, err := jsonparser.GetString(translated, "choices", "[0]", "delta", "content"); err == nil {
					responseBuilder.WriteString(content)
					tokenEstimate++
				} else if reasoning, err := jsonparser.GetString(translated, "choices", "[0]", "delta", "reasoning_content"); err == nil {
					responseBuilder.WriteString(reasoning)
					tokenEstimate++
				}

				if writeErr := sw.writeDataEvent(translated); writeErr != nil {
					sp.logger.Debug("client disconnected during stream",
						zap.String("provider", provider),
						zap.Error(writeErr),
					)
					responseText = responseBuilder.String()
					completionTokens = tokenEstimate
					return
				}
			}
			continue
		}

		// Handle non-data SSE events (some providers send event types)
		if bytes.HasPrefix(line, []byte("event: ")) {
			eventType := string(line[7:])
			sseEventType = eventType
			if provider == "anthropic" || provider == "1minai" || provider == "agentrouter" {
				tmx.SetEventType(eventType)
			}
			continue
		}

		// Handle provider-specific non-SSE streaming (e.g., Gemini JSON array)
		if provider == "gemini" && len(line) > 0 && (line[0] == '[' || line[0] == ',' || line[0] == '{') {
			text, tok := sp.handleGeminiStream(sw, tmx, line, scanner, progress)
			responseBuilder.WriteString(text)
			tokenEstimate += tok
			responseText = responseBuilder.String()
			completionTokens = tokenEstimate
			return
		}

		// Handle Ollama native NDJSON streaming (/api/chat and /api/generate).
		if provider == "ollama" && transmux.IsOllamaNativeChunk(line) {
			content := sp.processOllamaChunk(sw, tmx, line, progress)
			if content != "" {
				responseBuilder.WriteString(content)
				tokenEstimate++
			}
			continue
		}

		// Forward any other lines as-is (comments, retry directives, etc.)
		if bytes.HasPrefix(line, []byte(":")) {
			continue
		}
	}

	// ── Upstream stream failure: terminate cleanly and visibly ──────────────
	// scanner.Err() != nil means the upstream connection broke mid-stream
	// (unexpected EOF, connection reset, watchdog abort, …). The client
	// already holds partial content, so a retry is impossible — but the old
	// behavior of masking the truncation as a successful [DONE] left clients
	// with silently truncated answers and left nothing in production logs.
	if scanErr := scanner.Err(); scanErr != nil {
		elapsed := time.Since(streamStart)
		switch {
		case atomic.LoadInt32(&clientGone) == 1:
			// Client (or an intermediary in front of it) already tore the
			// connection down — nothing we write can be delivered.
			sp.logger.Warn("client disconnected during upstream stream — aborting relay",
				zap.String("provider", provider),
				zap.String("model", requestedModel),
				zap.Duration("elapsed", elapsed),
				zap.Int("estimated_tokens", tokenEstimate),
				zap.Error(scanErr),
			)
		case atomic.LoadInt32(&stallAborted) == 1:
			sp.logger.Warn("upstream stream stalled — terminating client stream with error event",
				zap.String("provider", provider),
				zap.String("model", requestedModel),
				zap.Duration("silent_for", streamStallTimeout),
				zap.Duration("elapsed", elapsed),
				zap.Int("estimated_tokens", tokenEstimate),
			)
			emitStreamInterrupted(sw, "upstream produced no data for "+streamStallTimeout.String())
		default:
			sp.logger.Warn("upstream stream interrupted mid-response — terminating client stream with error event",
				zap.String("provider", provider),
				zap.String("model", requestedModel),
				zap.Duration("elapsed", elapsed),
				zap.Int("estimated_tokens", tokenEstimate),
				zap.Error(scanErr),
			)
			emitStreamInterrupted(sw, scanErr.Error())
		}
		responseText = responseBuilder.String()
		completionTokens = tokenEstimate
		return
	}

	// Gap 5 Fix: For 1min.ai, emit a synthetic stop chunk if the upstream
	// connection dropped before the "done" event was transmitted. Without this,
	// downstream clients hang waiting for the terminal finish_reason marker.
	// If the "done" event was already processed, a duplicate stop chunk is
	// harmless — OpenAI clients handle multiple finish_reason chunks gracefully.
	if provider == "1minai" {
		tmx.SetEventType("done")
		stopChunk, _ := tmx.TranslateChunk([]byte(`{}`))
		if len(stopChunk) > 0 {
			_ = sw.writeDataEvent(stopChunk)
		}
	}

	// AgentRouter GPT models: the Anthropic SSE stream sometimes ends without
	// a message_delta event, so the client never receives a finish_reason.
	// Without finish_reason: "tool_calls", IDE clients don't execute tool calls.
	// Inject a synthetic finish_reason based on whether tool calls were seen.
	if (provider == "agentrouter" || provider == "anthropic") && !sawFinishReason {
		reason := "stop"
		if sawToolCalls {
			reason = "tool_calls"
		}
		finishChunk := fmt.Sprintf(`{"id":"chatcmpl-gate","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"%s"}]}`, reason)
		sp.logger.Info("injecting synthetic finish_reason",
			zap.String("provider", provider),
			zap.String("reason", reason),
			zap.Bool("saw_tool_calls", sawToolCalls),
		)
		_ = sw.writeDataEvent([]byte(finishChunk))
	}

	// Ensure [DONE] is sent even if upstream didn't send it
	_ = sw.writeDataEvent([]byte("[DONE]"))

	responseText = responseBuilder.String()
	completionTokens = tokenEstimate
	return
}

// handleGeminiStream processes Gemini's non-SSE JSON streaming format.
func (sp *StreamProxy) handleGeminiStream(sw *sseWriter, tmx transmux.Transmuxer, firstLine []byte, scanner *bufio.Scanner, progress chan struct{}) (string, int) {
	var sb strings.Builder
	var tokens int

	// Process the first line
	if val := sp.processGeminiChunk(sw, tmx, firstLine, progress); val != "" {
		sb.WriteString(val)
		tokens++
	}

	// Continue reading
	for scanner.Scan() {
		signalProgressChan(progress)
		line := scanner.Bytes()
		if len(line) == 0 || bytes.Equal(line, []byte("]")) {
			continue
		}
		if val := sp.processGeminiChunk(sw, tmx, line, progress); val != "" {
			sb.WriteString(val)
			tokens++
		}
	}

	_ = sw.writeDataEvent([]byte("[DONE]"))

	return sb.String(), tokens
}

// processOllamaChunk translates a single Ollama native NDJSON line into an
// OpenAI-compatible SSE chunk and flushes it to the client.
func (sp *StreamProxy) processOllamaChunk(sw *sseWriter, tmx transmux.Transmuxer, chunk []byte, progress chan struct{}) string {
	translated, err := tmx.TranslateChunk(chunk)
	if err != nil {
		sp.logger.Debug("ollama chunk transmux error", zap.Error(err))
		return ""
	}

	if len(translated) > 0 {
		signalProgressChan(progress)
		if writeErr := sw.writeDataEvent(translated); writeErr != nil {
			return ""
		}

		if content, err := jsonparser.GetString(translated, "choices", "[0]", "delta", "content"); err == nil {
			return content
		} else if reasoning, err := jsonparser.GetString(translated, "choices", "[0]", "delta", "reasoning_content"); err == nil {
			return reasoning
		}
	}
	return ""
}

// processGeminiChunk translates a single Gemini JSON chunk into OpenAI SSE format.
func (sp *StreamProxy) processGeminiChunk(sw *sseWriter, tmx transmux.Transmuxer, chunk []byte, progress chan struct{}) string {
	chunk = bytes.TrimLeft(chunk, "[,")
	chunk = bytes.TrimRight(chunk, "]")
	chunk = bytes.TrimSpace(chunk)

	if len(chunk) == 0 {
		return ""
	}

	translated, err := tmx.TranslateChunk(chunk)
	if err != nil {
		return ""
	}

	if len(translated) > 0 {
		signalProgressChan(progress)
		if writeErr := sw.writeDataEvent(translated); writeErr != nil {
			return ""
		}

		if content, err := jsonparser.GetString(translated, "choices", "[0]", "delta", "content"); err == nil {
			return content
		} else if reasoning, err := jsonparser.GetString(translated, "choices", "[0]", "delta", "reasoning_content"); err == nil {
			return reasoning
		}
	}
	return ""
}

// ExtractStreamFlag checks the body for the stream flag without full unmarshalling.
// Uses strings.Contains for minimal overhead.
func ExtractStreamFlag(body []byte) bool {
	return strings.Contains(string(body), `"stream":true`) ||
		strings.Contains(string(body), `"stream": true`)
}

// StreamBodyReader wraps an io.ReadCloser to tee into a buffer for retry scenarios.
type StreamBodyReader struct {
	io.ReadCloser
	buf *bytes.Buffer
}

func NewStreamBodyReader(body io.ReadCloser) *StreamBodyReader {
	return &StreamBodyReader{
		ReadCloser: body,
		buf:        &bytes.Buffer{},
	}
}

func (r *StreamBodyReader) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	if n > 0 {
		r.buf.Write(p[:n])
	}
	return n, err
}
