package transmux

import (
	"fmt"
	"strings"

	"github.com/buger/jsonparser"
)

// GeminiTransmuxer translates Google AI Studio's streamGenerateContent SSE format
// into OpenAI-compatible SSE chunks.
//
// Gemini streaming chunks look like:
//
//	{
//	  "candidates": [{
//	    "content": {
//	      "parts": [
//	        {"text": "answer text"},
//	        {"thought": true, "text": "thinking text"},
//	        {"functionCall": {"name": "fn", "args": {...}}}
//	      ],
//	      "role": "model"
//	    },
//	    "finishReason": "STOP"
//	  }],
//	  "usageMetadata": {
//	    "promptTokenCount": 10,
//	    "candidatesTokenCount": 20,
//	    "thoughtsTokenCount": 50
//	  }
//	}
//
// Each chunk is translated to one or more OpenAI SSE data lines.
// The transmuxer handles multi-part chunks (text + thinking + function calls),
// accumulates tool call deltas, and emits proper finish reason and usage chunks.
type GeminiTransmuxer struct {
	chunkIndex    int
	toolCallIndex int // increments per unique function call observed
}

// NewGeminiTransmuxer creates a new Gemini format translator.
func NewGeminiTransmuxer() *GeminiTransmuxer {
	return &GeminiTransmuxer{}
}

// TranslateChunk converts a Gemini streaming response chunk to OpenAI SSE format.
//
// A single Gemini chunk can contain multiple parts — for example, a thinking part
// followed by a text part, or a text part followed by a function call. This method
// iterates all parts and emits the appropriate OpenAI delta for each.
func (t *GeminiTransmuxer) TranslateChunk(data []byte) ([]byte, error) {
	t.chunkIndex++

	// ── Error body detection ──────────────────────────────────────────────────
	// Gemini can embed errors inside 200-status SSE streams (e.g. safety blocks
	// mid-generation, quota exceeded mid-stream).
	if errorMsg, _, _, err := jsonparser.Get(data, "error", "message"); err == nil && len(errorMsg) > 0 {
		errText, _ := jsonparser.GetString(data, "error", "message")
		return buildDelta([]byte(errText), false, "stop"), nil
	}

	// ── Extract finish reason and usage ───────────────────────────────────────
	finishReasonRaw, _, _, _ := jsonparser.Get(data, "candidates", "[0]", "finishReason")
	rawReason := strings.Trim(string(finishReasonRaw), `"`)
	fr := mapGeminiFinishReason(rawReason)

	promptTokens, _ := jsonparser.GetInt(data, "usageMetadata", "promptTokenCount")
	candidateTokens, _ := jsonparser.GetInt(data, "usageMetadata", "candidatesTokenCount")
	thoughtsTokens, _ := jsonparser.GetInt(data, "usageMetadata", "thoughtsTokenCount")

	// ── Iterate all parts in the chunk ────────────────────────────────────────
	partsRaw, _, _, err := jsonparser.Get(data, "candidates", "[0]", "content", "parts")
	if err != nil || len(partsRaw) == 0 {
		// No parts — emit finish/usage chunk if finish reason is present
		if fr != "" {
			return t.buildFinishChunk(fr, int(promptTokens), int(candidateTokens+thoughtsTokens)), nil
		}
		return nil, nil
	}

	// Collect output chunks from each part into a result buffer.
	var result []byte
	first := true

	_, _ = jsonparser.ArrayEach(partsRaw, func(partData []byte, _ jsonparser.ValueType, _ int, parseErr error) {
		if parseErr != nil {
			return
		}

		var chunk []byte

		// Check for thought flag (thinking/reasoning content)
		isThought, _ := jsonparser.GetBoolean(partData, "thought")

		// Text content (regular or thinking)
		if textRaw, _, _, tErr := jsonparser.Get(partData, "text"); tErr == nil && len(textRaw) > 0 {
			chunk = buildDelta(textRaw, isThought, "")
		} else if fnName, fnErr := jsonparser.GetString(partData, "functionCall", "name"); fnErr == nil && fnName != "" {
			// Function call part → tool call delta
			argsRaw, _, _, _ := jsonparser.Get(partData, "functionCall", "args")
			if len(argsRaw) == 0 {
				argsRaw = []byte("{}")
			}
			callID := fmt.Sprintf("call_%d", t.toolCallIndex)
			t.toolCallIndex++
			chunk = buildGeminiToolCallDeltaLocal(t.toolCallIndex-1, callID, fnName, string(argsRaw), "")
		}

		if len(chunk) > 0 {
			if !first {
				result = append(result, "\n\ndata: "...)
			}
			result = append(result, chunk...)
			first = false
		}
	})

	// ── Append finish/usage chunk if this is the terminal chunk ───────────────
	if fr != "" {
		finishChunk := t.buildFinishChunk(fr, int(promptTokens), int(candidateTokens+thoughtsTokens))
		if len(finishChunk) > 0 {
			if !first {
				result = append(result, "\n\ndata: "...)
			}
			result = append(result, finishChunk...)
		}
	}

	if len(result) == 0 {
		return nil, nil
	}
	return result, nil
}

// buildFinishChunk builds the terminal SSE chunk with finish reason and optional usage.
func (t *GeminiTransmuxer) buildFinishChunk(finishReason string, promptTokens, completionTokens int) []byte {
	if promptTokens > 0 || completionTokens > 0 {
		// Emit content-stop chunk + separate usage chunk
		stopChunk := buildDelta(nil, false, finishReason)
		usageChunk := buildUsageDelta(finishReason, promptTokens, completionTokens)
		result := make([]byte, 0, len(stopChunk)+len(usageChunk)+14)
		result = append(result, stopChunk...)
		result = append(result, "\n\ndata: "...)
		result = append(result, usageChunk...)
		return result
	}
	return buildDelta(nil, false, finishReason)
}

// SetEventType is not used for Gemini (no named event-based SSE).
func (t *GeminiTransmuxer) SetEventType(_ string) {}

// Close releases resources (no-op for GeminiTransmuxer).
func (t *GeminiTransmuxer) Close() {}

// mapGeminiFinishReason converts Gemini finish reasons to OpenAI format.
func mapGeminiFinishReason(reason string) string {
	switch reason {
	case "STOP":
		return "stop"
	case "MAX_TOKENS":
		return "length"
	case "SAFETY", "RECITATION", "PROHIBITED_CONTENT", "SPII":
		return "content_filter"
	case "MALFORMED_FUNCTION_CALL":
		return "stop"
	case "OTHER", "":
		return ""
	default:
		if reason == "" {
			return ""
		}
		return "stop"
	}
}

// buildGeminiToolCallDeltaLocal builds an OpenAI-compatible SSE chunk for a
// function call event emitted by Gemini in streaming mode. This is a local
// version that mirrors buildGeminiToolCallDelta in proxy/gemini.go but lives
// in the transmux package to avoid a circular dependency.
func buildGeminiToolCallDeltaLocal(index int, callID, funcName, argsJSON string, finishReason string) []byte {
	var sb strings.Builder
	sb.WriteString(`{"id":"chatcmpl-gate","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"tool_calls":[{"index":`)
	sb.WriteString(fmt.Sprintf("%d", index))
	if callID != "" {
		sb.WriteString(`,"id":`)
		sb.WriteString(jsonQuote(callID))
	}
	sb.WriteString(`,"type":"function","function":{"name":`)
	sb.WriteString(jsonQuote(funcName))
	sb.WriteString(`,"arguments":`)
	sb.WriteString(jsonQuote(argsJSON))
	sb.WriteString(`}}]}`)
	if finishReason != "" {
		sb.WriteString(`,"finish_reason":"`)
		sb.WriteString(finishReason)
		sb.WriteByte('"')
	} else {
		sb.WriteString(`,"finish_reason":null`)
	}
	sb.WriteString(`}]}`)
	return []byte(sb.String())
}

// jsonQuote wraps s in JSON double quotes with minimal escaping.
func jsonQuote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	s = strings.ReplaceAll(s, "\r", `\r`)
	s = strings.ReplaceAll(s, "\t", `\t`)
	return `"` + s + `"`
}
