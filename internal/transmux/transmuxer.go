package transmux

import (
	"bytes"
	"sync"
)

// Transmuxer defines the interface for provider-specific stream format translators.
// Each implementation translates a provider's streaming format into OpenAI-compatible
// SSE chunks, enabling IDE clients to work with any backend provider transparently.
type Transmuxer interface {
	// TranslateChunk takes a raw SSE data payload from the provider and returns
	// an OpenAI-compatible JSON chunk. Returns empty slice to skip the chunk.
	TranslateChunk(data []byte) ([]byte, error)

	// SetEventType is called when an SSE "event: " line is received.
	// Used by providers like Anthropic that use event-type based streaming.
	SetEventType(eventType string)

	// Close releases any pooled resources.
	Close()
}

// TransmuxState tracks the reasoning/thinking state machine position.
type TransmuxState int

const (
	StateNormal       TransmuxState = iota // Regular content
	StateInsideThink                        // Inside <think>...</think> tags
)

// NewTransmuxer creates the appropriate transmuxer for a provider.
func NewTransmuxer(provider string) Transmuxer {
	switch provider {
	case "anthropic":
		return NewAnthropicTransmuxer()
	case "gemini", "vertex":
		return NewGeminiTransmuxer()
	case "nvidia":
		return NewNvidiaTransmuxer()
	case "ollama":
		// Ollama Cloud uses native NDJSON streaming (/api/chat, /api/generate).
		// Local Ollama in OpenAI-compat mode also benefits from this since it
		// can emit native NDJSON when called via /api/* endpoints.
		return NewOllamaTransmuxer()
	case "1minai":
		// 1min.ai uses named SSE events (content/result/done/error).
		return NewOneMinAITransmuxer()
	case "zenmux":
		return NewOpenAITransmuxer()
	default:
		// OpenAI-compatible providers (openai, deepseek, groq, together, etc.)
		return NewOpenAITransmuxer()
	}
}

// --- Shared buffer pool for all transmuxers ---

var chunkBufPool = sync.Pool{
	New: func() interface{} {
		return bytes.NewBuffer(make([]byte, 0, 4096))
	},
}

func getBuffer() *bytes.Buffer {
	buf := chunkBufPool.Get().(*bytes.Buffer)
	buf.Reset()
	return buf
}

func putBuffer(buf *bytes.Buffer) {
	chunkBufPool.Put(buf)
}

// --- Shared helper functions ---

// buildDelta constructs an OpenAI-compatible SSE chunk JSON.
// This is a high-speed byte-level constructor that avoids JSON marshalling.
func buildDelta(content []byte, isReasoning bool, finishReason string) []byte {
	buf := getBuffer()
	defer putBuffer(buf)

	buf.WriteString(`{"id":"chatcmpl-gate","object":"chat.completion.chunk","choices":[{"index":0,"delta":{`)

	if len(content) > 0 {
		if isReasoning {
			buf.WriteString(`"reasoning_content":`)
		} else {
			buf.WriteString(`"content":`)
		}
		// Write JSON-escaped string value
		buf.WriteByte('"')
		escapeJSONString(buf, content)
		buf.WriteByte('"')
	}

	buf.WriteString(`}`)

	if finishReason != "" {
		buf.WriteString(`,"finish_reason":"`)
		buf.WriteString(finishReason)
		buf.WriteByte('"')
	} else {
		buf.WriteString(`,"finish_reason":null`)
	}

	buf.WriteString(`}]}`)

	// Copy result to avoid returning pooled buffer
	result := make([]byte, buf.Len())
	copy(result, buf.Bytes())
	return result
}

// buildUsageDelta constructs a final chunk with token usage information.
func buildUsageDelta(finishReason string, promptTokens, completionTokens int) []byte {
	buf := getBuffer()
	defer putBuffer(buf)

	buf.WriteString(`{"id":"chatcmpl-gate","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"`)
	buf.WriteString(finishReason)
	buf.WriteString(`"}],"usage":{"prompt_tokens":`)
	writeInt(buf, promptTokens)
	buf.WriteString(`,"completion_tokens":`)
	writeInt(buf, completionTokens)
	buf.WriteString(`,"total_tokens":`)
	writeInt(buf, promptTokens+completionTokens)
	buf.WriteString(`}}`)

	result := make([]byte, buf.Len())
	copy(result, buf.Bytes())
	return result
}

// escapeJSONString writes a JSON-escaped version of s into buf.
// This handles the minimum required escaping without reflection.
func escapeJSONString(buf *bytes.Buffer, s []byte) {
	for _, b := range s {
		switch b {
		case '"':
			buf.WriteString(`\"`)
		case '\\':
			buf.WriteString(`\\`)
		case '\n':
			buf.WriteString(`\n`)
		case '\r':
			buf.WriteString(`\r`)
		case '\t':
			buf.WriteString(`\t`)
		default:
			if b < 0x20 {
				// Control characters
				buf.WriteString(`\u00`)
				buf.WriteByte("0123456789abcdef"[b>>4])
				buf.WriteByte("0123456789abcdef"[b&0xf])
			} else {
				buf.WriteByte(b)
			}
		}
	}
}

// writeInt writes an integer to a buffer without fmt.Sprintf allocation.
func writeInt(buf *bytes.Buffer, n int) {
	if n == 0 {
		buf.WriteByte('0')
		return
	}
	if n < 0 {
		buf.WriteByte('-')
		n = -n
	}
	// Max int digits
	var digits [20]byte
	pos := len(digits)
	for n > 0 {
		pos--
		digits[pos] = byte('0' + n%10)
		n /= 10
	}
	buf.Write(digits[pos:])
}
