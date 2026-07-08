package transmux

import (
	"github.com/buger/jsonparser"
)

// AnthropicTransmuxer translates Anthropic's event-based SSE format
// into OpenAI-compatible SSE chunks.
//
// Anthropic's streaming format uses event types:
//   - message_start       → initial message metadata
//   - content_block_start → beginning of a content block
//   - content_block_delta → incremental content (text_delta or thinking_delta)
//   - content_block_stop  → end of a content block
//   - message_delta       → final message metadata (stop_reason, usage)
//   - message_stop        → stream termination
//   - ping                → keepalive
type AnthropicTransmuxer struct {
	eventType  string
	blockType  string // "text" or "thinking"
}

// NewAnthropicTransmuxer creates a new Anthropic format translator.
func NewAnthropicTransmuxer() *AnthropicTransmuxer {
	return &AnthropicTransmuxer{}
}

// SetEventType records the current SSE event type for the next data chunk.
func (t *AnthropicTransmuxer) SetEventType(eventType string) {
	t.eventType = eventType
}

// TranslateChunk converts an Anthropic SSE data payload to OpenAI format.
func (t *AnthropicTransmuxer) TranslateChunk(data []byte) ([]byte, error) {
	switch t.eventType {
	case "message_start":
		// Initial message — we could extract the model info, but OpenAI format
		// doesn't require it in streaming. Skip.
		return nil, nil

	case "content_block_start":
		// Detect block type: "text" or "thinking"
		blockType, _, _, err := jsonparser.Get(data, "content_block", "type")
		if err == nil {
			t.blockType = string(blockType)
		}

		// For thinking blocks, extract initial thinking text if present
		if t.blockType == "thinking" {
			thinking, _, _, err := jsonparser.Get(data, "content_block", "thinking")
			if err == nil && len(thinking) > 0 {
				return buildDelta(thinking, true, ""), nil
			}
		}

		// For text blocks, extract initial text if present
		if t.blockType == "text" {
			text, _, _, err := jsonparser.Get(data, "content_block", "text")
			if err == nil && len(text) > 0 {
				return buildDelta(text, false, ""), nil
			}
		}

		return nil, nil

	case "content_block_delta":
		// Extract the delta content based on delta type
		deltaType, _, _, _ := jsonparser.Get(data, "delta", "type")

		switch string(deltaType) {
		case "thinking_delta":
			// Anthropic thinking delta → reasoning_content
			thinking, _, _, err := jsonparser.Get(data, "delta", "thinking")
			if err == nil && len(thinking) > 0 {
				return buildDelta(thinking, true, ""), nil
			}

		case "text_delta":
			// Standard text delta → content
			text, _, _, err := jsonparser.Get(data, "delta", "text")
			if err == nil && len(text) > 0 {
				return buildDelta(text, false, ""), nil
			}

		case "input_json_delta":
			// Tool use delta — forward as content for now
			partial, _, _, err := jsonparser.Get(data, "delta", "partial_json")
			if err == nil && len(partial) > 0 {
				return buildDelta(partial, false, ""), nil
			}
		}
		return nil, nil

	case "content_block_stop":
		// Block finished — no output needed
		t.blockType = ""
		return nil, nil

	case "message_delta":
		// Extract stop_reason and usage
		stopReason, _, _, _ := jsonparser.Get(data, "delta", "stop_reason")

		// Try to extract usage
		inputTokens, _ := jsonparser.GetInt(data, "usage", "input_tokens")
		outputTokens, _ := jsonparser.GetInt(data, "usage", "output_tokens")

		reason := mapAnthropicStopReason(string(stopReason))

		if inputTokens > 0 || outputTokens > 0 {
			return buildUsageDelta(reason, int(inputTokens), int(outputTokens)), nil
		}

		return buildDelta(nil, false, reason), nil

	case "message_stop":
		// Stream termination — the [DONE] marker is handled by the stream proxy
		return nil, nil

	case "ping":
		// Keepalive — no output
		return nil, nil

	case "error":
		// Error event — pass through as content for visibility
		errMsg, _, _, _ := jsonparser.Get(data, "error", "message")
		if len(errMsg) > 0 {
			return buildDelta(errMsg, false, ""), nil
		}
		return nil, nil

	default:
		// Unknown event type — skip
		return nil, nil
	}
}

// Close releases resources.
func (t *AnthropicTransmuxer) Close() {}

// mapAnthropicStopReason converts Anthropic stop reasons to OpenAI format.
func mapAnthropicStopReason(reason string) string {
	switch reason {
	case "end_turn":
		return "stop"
	case "max_tokens":
		return "length"
	case "stop_sequence":
		return "stop"
	case "tool_use":
		return "tool_calls"
	default:
		if reason == "" {
			return "stop"
		}
		return reason
	}
}
