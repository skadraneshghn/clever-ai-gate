package transmux

import (
	"fmt"

	"github.com/buger/jsonparser"
)

// OneMinAITransmuxer handles the 1min.ai SSE streaming format.
//
// 1min.ai uses named SSE events for streaming:
//
//	event: content
//	data: {"content": "Hello"}
//
//	event: content
//	data: {"content": " world"}
//
//	event: result
//	data: {"aiRecord": {...}}
//
//	event: done
//	data: {"message": "Stream completed"}
//
// Event types:
//   - content: incremental text chunk → translated to OpenAI delta
//   - result:  final full record → skipped (not needed for streaming)
//   - done:    stream complete → translated to OpenAI finish_reason "stop"
//   - error:   stream error → returns error
type OneMinAITransmuxer struct {
	eventType string
}

// NewOneMinAITransmuxer creates a transmuxer for the 1min.ai streaming API.
func NewOneMinAITransmuxer() *OneMinAITransmuxer {
	return &OneMinAITransmuxer{}
}

// TranslateChunk converts a 1min.ai SSE data payload into an OpenAI-compatible
// SSE chunk. The behaviour depends on the current event type (set via
// SetEventType when an "event:" line is received by the stream proxy).
func (t *OneMinAITransmuxer) TranslateChunk(data []byte) ([]byte, error) {
	switch t.eventType {
	case "content":
		// Extract the "content" field from the JSON data
		content, err := jsonparser.GetString(data, "content")
		if err != nil || content == "" {
			// Skip empty content chunks
			return nil, nil
		}
		return buildDelta([]byte(content), false, ""), nil

	case "done":
		// Stream complete — emit a final chunk with finish_reason "stop"
		return buildDelta([]byte{}, false, "stop"), nil

	case "result":
		// Final full record — not needed for streaming, skip
		return nil, nil

	case "error":
		// Stream error — extract message if available
		msg, _ := jsonparser.GetString(data, "message")
		if msg == "" {
			msg = string(data)
		}
		return nil, fmt.Errorf("1min.ai stream error: %s", msg)

	default:
		// Unknown event type — try to extract content as fallback
		if content, err := jsonparser.GetString(data, "content"); err == nil && content != "" {
			return buildDelta([]byte(content), false, ""), nil
		}
		return nil, nil
	}
}

// SetEventType is called when an SSE "event:" line is received.
// The stream proxy calls this before the corresponding "data:" line.
func (t *OneMinAITransmuxer) SetEventType(eventType string) {
	t.eventType = eventType
}

// Close is a no-op for the 1min.ai transmuxer.
func (t *OneMinAITransmuxer) Close() {}
