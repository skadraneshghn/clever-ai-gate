package transmux

import (
	"bytes"

	"github.com/buger/jsonparser"
)

// OpenAITransmuxer handles OpenAI and OpenAI-compatible providers.
// For these providers, the format is already correct — we only need to:
// 1. Detect and transform <think>...</think> tags into reasoning_content
// 2. Pass through everything else as-is
type OpenAITransmuxer struct {
	state TransmuxState
}

// NewOpenAITransmuxer creates a transmuxer for OpenAI-compatible providers.
func NewOpenAITransmuxer() *OpenAITransmuxer {
	return &OpenAITransmuxer{
		state: StateNormal,
	}
}

// TranslateChunk processes an OpenAI SSE data chunk.
// For standard OpenAI responses, this is a near-zero-cost passthrough.
// For providers like DeepSeek that use <think> tags, it extracts reasoning content.
func (t *OpenAITransmuxer) TranslateChunk(data []byte) ([]byte, error) {
	// Check if this chunk already has reasoning_content (DeepSeek native format)
	if reasoningContent, _, _, err := jsonparser.Get(data, "choices", "[0]", "delta", "reasoning_content"); err == nil && len(reasoningContent) > 0 {
		// Already in correct format — passthrough
		return data, nil
	}

	// Extract content from delta
	content, _, _, err := jsonparser.Get(data, "choices", "[0]", "delta", "content")
	if err != nil || len(content) == 0 {
		// No content delta — might be a role/finish_reason chunk, passthrough
		return data, nil
	}

	// Check for <think> tag transitions in content
	contentStr := content

	switch t.state {
	case StateNormal:
		if bytes.Contains(contentStr, []byte("<think>")) {
			t.state = StateInsideThink
			// Extract content after <think> tag
			after := bytes.SplitN(contentStr, []byte("<think>"), 2)
			if len(after) > 1 && len(after[1]) > 0 {
				return buildDelta(after[1], true, ""), nil
			}
			// Just the opening tag, no content yet
			return nil, nil
		}
		// Normal content — passthrough
		return data, nil

	case StateInsideThink:
		if bytes.Contains(contentStr, []byte("</think>")) {
			t.state = StateNormal
			// Extract content before </think> tag
			before := bytes.SplitN(contentStr, []byte("</think>"), 2)

			// If there's reasoning content before the tag, emit it
			if len(before[0]) > 0 {
				return buildDelta(before[0], true, ""), nil
			}

			// If there's normal content after the tag
			if len(before) > 1 && len(before[1]) > 0 {
				return buildDelta(before[1], false, ""), nil
			}
			return nil, nil
		}
		// Still inside thinking — convert to reasoning_content
		return buildDelta(contentStr, true, ""), nil
	}

	return data, nil
}

// SetEventType is a no-op for OpenAI-compatible providers.
func (t *OpenAITransmuxer) SetEventType(_ string) {}

// Close is a no-op for OpenAI-compatible providers.
func (t *OpenAITransmuxer) Close() {}
