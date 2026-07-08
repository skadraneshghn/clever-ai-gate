package transmux

import (
	"github.com/buger/jsonparser"
)

// GeminiTransmuxer translates Google Gemini's REST streaming format
// into OpenAI-compatible SSE chunks.
//
// Gemini's response structure:
//
//	{
//	  "candidates": [{
//	    "content": {
//	      "parts": [{"text": "..."}],
//	      "role": "model"
//	    },
//	    "finishReason": "STOP"
//	  }],
//	  "usageMetadata": {
//	    "promptTokenCount": 10,
//	    "candidatesTokenCount": 20
//	  }
//	}
type GeminiTransmuxer struct {
	chunkIndex int
}

// NewGeminiTransmuxer creates a new Gemini format translator.
func NewGeminiTransmuxer() *GeminiTransmuxer {
	return &GeminiTransmuxer{}
}

// TranslateChunk converts a Gemini response chunk to OpenAI SSE format.
func (t *GeminiTransmuxer) TranslateChunk(data []byte) ([]byte, error) {
	t.chunkIndex++

	// Check for error response
	errorMsg, _, _, err := jsonparser.Get(data, "error", "message")
	if err == nil && len(errorMsg) > 0 {
		return buildDelta(errorMsg, false, ""), nil
	}

	// Extract text from candidates[0].content.parts[0].text
	text, _, _, err := jsonparser.Get(data, "candidates", "[0]", "content", "parts", "[0]", "text")
	if err != nil || len(text) == 0 {
		// Try thought/reasoning from Gemini 2.0 thinking models
		thought, _, _, tErr := jsonparser.Get(data, "candidates", "[0]", "content", "parts", "[0]", "thought")
		if tErr == nil && len(thought) > 0 {
			return buildDelta(thought, true, ""), nil
		}

		// No text content — check for finish reason
		finishReason, _, _, frErr := jsonparser.Get(data, "candidates", "[0]", "finishReason")
		if frErr == nil && len(finishReason) > 0 {
			reason := mapGeminiFinishReason(string(finishReason))

			// Extract usage metadata
			promptTokens, _ := jsonparser.GetInt(data, "usageMetadata", "promptTokenCount")
			completionTokens, _ := jsonparser.GetInt(data, "usageMetadata", "candidatesTokenCount")

			if promptTokens > 0 || completionTokens > 0 {
				return buildUsageDelta(reason, int(promptTokens), int(completionTokens)), nil
			}
			return buildDelta(nil, false, reason), nil
		}

		return nil, nil
	}

	// Check finish reason to determine if this is the last chunk
	finishReason, _, _, _ := jsonparser.Get(data, "candidates", "[0]", "finishReason")
	fr := ""
	if len(finishReason) > 0 && string(finishReason) != "null" {
		fr = mapGeminiFinishReason(string(finishReason))
	}

	if fr != "" {
		// Last chunk with content — include usage if available
		promptTokens, _ := jsonparser.GetInt(data, "usageMetadata", "promptTokenCount")
		completionTokens, _ := jsonparser.GetInt(data, "usageMetadata", "candidatesTokenCount")

		if promptTokens > 0 || completionTokens > 0 {
			// Build chunk with content
			contentChunk := buildDelta(text, false, "")
			// Build usage chunk
			usageChunk := buildUsageDelta(fr, int(promptTokens), int(completionTokens))

			// Concatenate with SSE delimiter
			result := make([]byte, 0, len(contentChunk)+len(usageChunk)+14)
			result = append(result, contentChunk...)
			result = append(result, "\n\ndata: "...)
			result = append(result, usageChunk...)
			return result, nil
		}

		return buildDelta(text, false, fr), nil
	}

	return buildDelta(text, false, ""), nil
}

// SetEventType is not used for Gemini (no event-based SSE).
func (t *GeminiTransmuxer) SetEventType(_ string) {}

// Close releases resources.
func (t *GeminiTransmuxer) Close() {}

// mapGeminiFinishReason converts Gemini finish reasons to OpenAI format.
func mapGeminiFinishReason(reason string) string {
	switch reason {
	case "STOP":
		return "stop"
	case "MAX_TOKENS":
		return "length"
	case "SAFETY":
		return "content_filter"
	case "RECITATION":
		return "content_filter"
	case "OTHER":
		return "stop"
	default:
		if reason == "" {
			return ""
		}
		return "stop"
	}
}
