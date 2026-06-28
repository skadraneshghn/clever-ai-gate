package proxy

import (
	"bufio"
	"bytes"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/skadraneshghn/clever-ai-gate/internal/transmux"
	"go.uber.org/zap"
)

// StreamProxy handles SSE (Server-Sent Events) streaming from upstream providers.
// It reads upstream chunks line-by-line, passes them through a provider-specific
// transmuxer for format normalization, and flushes each chunk to the client immediately.
type StreamProxy struct {
	client     *http.Client
	logger     *zap.Logger
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

// ProxyStream pipes SSE chunks from upstream to client with format translation.
// Each chunk is flushed immediately for minimum Time To First Token (TTFT).
//
// The flow:
//  1. Set SSE response headers
//  2. Create provider-specific transmuxer
//  3. Read upstream line-by-line with bufio.Scanner
//  4. For each "data: " prefixed line, transmux and flush
//  5. On [DONE], send final event and close
func (sp *StreamProxy) ProxyStream(c *gin.Context, upstream *http.Response, provider string) {
	defer upstream.Body.Close()

	// Step 1: Set SSE headers for streaming
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("Transfer-Encoding", "chunked")
	c.Writer.Header().Set("X-Accel-Buffering", "no") // Disable nginx buffering
	c.Writer.WriteHeader(http.StatusOK)

	// Step 2: Create the appropriate transmuxer for this provider
	tmx := transmux.NewTransmuxer(provider)

	// Step 3: Create scanner with pooled buffer
	scanner := bufio.NewScanner(upstream.Body)
	scanBuf := sp.scannerPool.Get().([]byte)
	defer sp.scannerPool.Put(scanBuf[:0])
	scanner.Buffer(scanBuf, 1024*1024) // Max 1MB line (for base64 images)

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		sp.logger.Error("response writer does not support flushing")
		return
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
				return
			}

			// Transmux the chunk to OpenAI format
			translated, err := tmx.TranslateChunk(data)
			if err != nil {
				sp.logger.Debug("transmux error, forwarding raw",
					zap.String("provider", provider),
					zap.Error(err),
				)
				// On transmux error, forward raw data
				translated = data
			}

			if len(translated) > 0 {
				c.Writer.Write([]byte("data: "))
				c.Writer.Write(translated)
				c.Writer.Write([]byte("\n\n"))
				flusher.Flush() // Flush after every chunk for minimum TTFT
			}
			continue
		}

		// Handle non-data SSE events (some providers send event types)
		if bytes.HasPrefix(line, []byte("event: ")) {
			eventType := string(line[7:])

			// Anthropic uses event-based SSE
			if provider == "anthropic" {
				tmx.SetEventType(eventType)
			}
			continue
		}

		// Handle provider-specific non-SSE streaming (e.g., Gemini JSON array)
		if provider == "gemini" && (line[0] == '[' || line[0] == ',' || line[0] == '{') {
			sp.handleGeminiStream(c, flusher, tmx, line, scanner)
			return
		}

		// Forward any other lines as-is (comments, retry directives, etc.)
		if bytes.HasPrefix(line, []byte(":")) {
			// SSE comment — skip
			continue
		}
	}

	if err := scanner.Err(); err != nil {
		sp.logger.Debug("stream scanner error", zap.Error(err))
	}

	// Ensure [DONE] is sent even if upstream didn't send it
	c.Writer.Write([]byte("data: [DONE]\n\n"))
	flusher.Flush()
}

// handleGeminiStream processes Gemini's non-SSE JSON streaming format.
// Gemini sends a JSON array of response objects, one per line.
func (sp *StreamProxy) handleGeminiStream(c *gin.Context, flusher http.Flusher, tmx transmux.Transmuxer, firstLine []byte, scanner *bufio.Scanner) {
	// Process the first line
	sp.processGeminiChunk(c, flusher, tmx, firstLine)

	// Continue reading
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 || bytes.Equal(line, []byte("]")) {
			continue
		}
		sp.processGeminiChunk(c, flusher, tmx, line)
	}

	c.Writer.Write([]byte("data: [DONE]\n\n"))
	flusher.Flush()
}

func (sp *StreamProxy) processGeminiChunk(c *gin.Context, flusher http.Flusher, tmx transmux.Transmuxer, chunk []byte) {
	// Strip leading comma or bracket from JSON array format
	chunk = bytes.TrimLeft(chunk, "[,")
	chunk = bytes.TrimRight(chunk, "]")
	chunk = bytes.TrimSpace(chunk)

	if len(chunk) == 0 {
		return
	}

	translated, err := tmx.TranslateChunk(chunk)
	if err != nil {
		return
	}

	if len(translated) > 0 {
		c.Writer.Write([]byte("data: "))
		c.Writer.Write(translated)
		c.Writer.Write([]byte("\n\n"))
		flusher.Flush()
	}
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
