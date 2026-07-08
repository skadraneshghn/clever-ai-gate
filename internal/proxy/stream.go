package proxy

import (
	"bufio"
	"bytes"
	"io"
	"net/http"
	"runtime/debug"
	"strings"
	"sync"

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

// ProxyStream pipes SSE chunks from upstream to client with format translation,
// and returns the fully accumulated response text along with estimated completion tokens.
func (sp *StreamProxy) ProxyStream(c *gin.Context, upstream *http.Response, provider string) (responseText string, completionTokens int) {
	// Acquire pooled scanner buffer
	scanBuf := sp.scannerPool.Get().([]byte)

	var responseBuilder strings.Builder
	var tokenEstimate int

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
			if flusher, ok := c.Writer.(http.Flusher); ok {
				c.Writer.Write([]byte("data: [DONE]\n\n"))
				flusher.Flush()
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

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		sp.logger.Error("response writer does not support flushing")
		return "", 0
	}

	// Step 4: Read and transmux each SSE line
	for scanner.Scan() {
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
				c.Writer.Write([]byte("data: [DONE]\n\n"))
				flusher.Flush()
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

			if len(translated) > 0 {
				if content, err := jsonparser.GetString(translated, "choices", "[0]", "delta", "content"); err == nil {
					responseBuilder.WriteString(content)
					tokenEstimate++
				}

				if _, writeErr := c.Writer.Write([]byte("data: ")); writeErr != nil {
					sp.logger.Debug("client disconnected during stream",
						zap.String("provider", provider),
						zap.Error(writeErr),
					)
					responseText = responseBuilder.String()
					completionTokens = tokenEstimate
					return
				}
				c.Writer.Write(translated)
				c.Writer.Write([]byte("\n\n"))
				flusher.Flush()
			}
			continue
		}

		// Handle non-data SSE events (some providers send event types)
		if bytes.HasPrefix(line, []byte("event: ")) {
			eventType := string(line[7:])
			if provider == "anthropic" || provider == "1minai" {
				tmx.SetEventType(eventType)
			}
			continue
		}

		// Handle provider-specific non-SSE streaming (e.g., Gemini JSON array)
		if provider == "gemini" && len(line) > 0 && (line[0] == '[' || line[0] == ',' || line[0] == '{') {
			text, tok := sp.handleGeminiStream(c, flusher, tmx, line, scanner)
			responseBuilder.WriteString(text)
			tokenEstimate += tok
			responseText = responseBuilder.String()
			completionTokens = tokenEstimate
			return
		}

		// Handle Ollama native NDJSON streaming (/api/chat and /api/generate).
		if provider == "ollama" && transmux.IsOllamaNativeChunk(line) {
			content := sp.processOllamaChunk(c, flusher, tmx, line)
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

	if err := scanner.Err(); err != nil {
		sp.logger.Debug("stream scanner error", zap.Error(err))
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
			c.Writer.Write([]byte("data: "))
			c.Writer.Write(stopChunk)
			c.Writer.Write([]byte("\n\n"))
			flusher.Flush()
		}
	}

	// Ensure [DONE] is sent even if upstream didn't send it
	c.Writer.Write([]byte("data: [DONE]\n\n"))
	flusher.Flush()

	responseText = responseBuilder.String()
	completionTokens = tokenEstimate
	return
}

// handleGeminiStream processes Gemini's non-SSE JSON streaming format.
func (sp *StreamProxy) handleGeminiStream(c *gin.Context, flusher http.Flusher, tmx transmux.Transmuxer, firstLine []byte, scanner *bufio.Scanner) (string, int) {
	var sb strings.Builder
	var tokens int

	// Process the first line
	if val := sp.processGeminiChunk(c, flusher, tmx, firstLine); val != "" {
		sb.WriteString(val)
		tokens++
	}

	// Continue reading
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 || bytes.Equal(line, []byte("]")) {
			continue
		}
		if val := sp.processGeminiChunk(c, flusher, tmx, line); val != "" {
			sb.WriteString(val)
			tokens++
		}
	}

	c.Writer.Write([]byte("data: [DONE]\n\n"))
	flusher.Flush()

	return sb.String(), tokens
}

// processOllamaChunk translates a single Ollama native NDJSON line into an
// OpenAI-compatible SSE chunk and flushes it to the client.
func (sp *StreamProxy) processOllamaChunk(c *gin.Context, flusher http.Flusher, tmx transmux.Transmuxer, chunk []byte) string {
	translated, err := tmx.TranslateChunk(chunk)
	if err != nil {
		sp.logger.Debug("ollama chunk transmux error", zap.Error(err))
		return ""
	}

	if len(translated) > 0 {
		c.Writer.Write([]byte("data: "))
		c.Writer.Write(translated)
		c.Writer.Write([]byte("\n\n"))
		flusher.Flush()

		if content, err := jsonparser.GetString(translated, "choices", "[0]", "delta", "content"); err == nil {
			return content
		}
	}
	return ""
}

// processGeminiChunk translates a single Gemini JSON chunk into OpenAI SSE format.
func (sp *StreamProxy) processGeminiChunk(c *gin.Context, flusher http.Flusher, tmx transmux.Transmuxer, chunk []byte) string {
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
		c.Writer.Write([]byte("data: "))
		c.Writer.Write(translated)
		c.Writer.Write([]byte("\n\n"))
		flusher.Flush()

		if content, err := jsonparser.GetString(translated, "choices", "[0]", "delta", "content"); err == nil {
			return content
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
