package transmux

import (
	"bytes"

	"github.com/buger/jsonparser"
)

// OllamaTransmuxer handles the native Ollama streaming format.
//
// Ollama Cloud (and local Ollama native mode) emits newline-delimited JSON
// (NDJSON) rather than SSE-prefixed data lines. Each line is a raw JSON object:
//
//	{"model":"llama4","message":{"role":"assistant","content":"Hi"},"done":false}
//	{"model":"llama4","message":{"role":"assistant","content":"!"},"done":false}
//	{"model":"llama4","done":true,"done_reason":"stop","prompt_eval_count":10,"eval_count":5}
//
// The /api/generate endpoint uses a different shape:
//
//	{"model":"llama4","response":"Hello","done":false}
//	{"model":"llama4","done":true,"done_reason":"stop"}
//
// This transmuxer converts both into OpenAI-compatible SSE chunks.
type OllamaTransmuxer struct{}

// NewOllamaTransmuxer creates a transmuxer for the native Ollama API.
func NewOllamaTransmuxer() *OllamaTransmuxer {
	return &OllamaTransmuxer{}
}

// TranslateChunk converts a native Ollama NDJSON chunk into an OpenAI-compatible
// SSE data payload. It handles both /api/chat and /api/generate response shapes.
func (t *OllamaTransmuxer) TranslateChunk(data []byte) ([]byte, error) {
	// --- /api/chat response: { "message": { "content": "..." }, "done": false }
	if content, _, _, err := jsonparser.Get(data, "message", "content"); err == nil {
		done, _ := jsonparser.GetBoolean(data, "done")
		finishReason := ""
		if done {
			// Try to get the done_reason for a proper finish_reason
			if reason, err := jsonparser.GetString(data, "done_reason"); err == nil && reason != "" {
				finishReason = reason
			} else {
				finishReason = "stop"
			}
		}
		return buildDelta(content, false, finishReason), nil
	}

	// --- /api/generate response: { "response": "...", "done": false }
	if response, _, _, err := jsonparser.Get(data, "response"); err == nil {
		done, _ := jsonparser.GetBoolean(data, "done")
		finishReason := ""
		if done {
			if reason, err := jsonparser.GetString(data, "done_reason"); err == nil && reason != "" {
				finishReason = reason
			} else {
				finishReason = "stop"
			}
		}
		return buildDelta(response, false, finishReason), nil
	}

	// --- Final "done" chunk with no content (usage-only) ---
	done, _ := jsonparser.GetBoolean(data, "done")
	if done {
		// Check for token usage fields (eval_count, prompt_eval_count)
		promptTokens, _ := jsonparser.GetInt(data, "prompt_eval_count")
		completionTokens, _ := jsonparser.GetInt(data, "eval_count")
		finishReason := "stop"
		if reason, err := jsonparser.GetString(data, "done_reason"); err == nil && reason != "" {
			finishReason = reason
		}
		if promptTokens > 0 || completionTokens > 0 {
			return buildUsageDelta(finishReason, int(promptTokens), int(completionTokens)), nil
		}
		// Emit a stop chunk with no content
		return buildDelta([]byte{}, false, finishReason), nil
	}

	// Unknown shape — try to pass through as a raw empty delta to avoid breaking the stream
	return nil, nil
}

// SetEventType is a no-op for Ollama (uses NDJSON, not SSE event types).
func (t *OllamaTransmuxer) SetEventType(_ string) {}

// Close is a no-op for Ollama transmuxer.
func (t *OllamaTransmuxer) Close() {}

// IsOllamaNativeChunk returns true when the line looks like an Ollama native
// NDJSON chunk — i.e., it is a raw JSON object starting with '{' and contains
// the "model" key, but is NOT prefixed with "data: ".
// This is used by the stream proxy to switch into NDJSON mode.
func IsOllamaNativeChunk(line []byte) bool {
	if len(line) < 2 || line[0] != '{' {
		return false
	}
	return bytes.Contains(line, []byte(`"model"`))
}
