package transmux

import (
	"bytes"

	"github.com/buger/jsonparser"
)

// NvidiaTransmuxer handles NVIDIA NIM streaming responses.
// NVIDIA models with enable_thinking=true stream reasoning tokens using either:
//   - A native "reasoning_content" field in the delta (similar to DeepSeek)
//   - Inline <think>...</think> tags within the "content" field
//
// This transmuxer normalizes both patterns into the OpenAI-compatible
// "reasoning_content" format that IDE extensions (Cline, Kilo) expect.
type NvidiaTransmuxer struct {
	state TransmuxState
}

// NewNvidiaTransmuxer creates a transmuxer for NVIDIA NIM providers.
func NewNvidiaTransmuxer() *NvidiaTransmuxer {
	return &NvidiaTransmuxer{
		state: StateNormal,
	}
}

// TranslateChunk processes an NVIDIA SSE data chunk.
//
// Processing order:
//  1. If the chunk already has a "reasoning_content" field → passthrough
//  2. If <think> tags are detected in "content" → extract to reasoning_content
//  3. All other content → passthrough as-is
func (t *NvidiaTransmuxer) TranslateChunk(data []byte) ([]byte, error) {
	// Check if this chunk already has reasoning_content (native NVIDIA format)
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
				// Check if the thinking block closes in the same chunk
				if bytes.Contains(after[1], []byte("</think>")) {
					parts := bytes.SplitN(after[1], []byte("</think>"), 2)
					t.state = StateNormal
					if len(parts[0]) > 0 {
						return buildDelta(parts[0], true, ""), nil
					}
					if len(parts) > 1 && len(parts[1]) > 0 {
						return buildDelta(parts[1], false, ""), nil
					}
					return nil, nil
				}
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

// SetEventType is a no-op for NVIDIA providers (they use standard SSE).
func (t *NvidiaTransmuxer) SetEventType(_ string) {}

// Close is a no-op for NVIDIA providers.
func (t *NvidiaTransmuxer) Close() {}
