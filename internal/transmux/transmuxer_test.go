package transmux

import (
	"encoding/json"
	"testing"
)

func TestOpenAIPassthrough(t *testing.T) {
	tmx := NewOpenAITransmuxer()
	defer tmx.Close()

	// Standard OpenAI chunk — should pass through unchanged
	chunk := []byte(`{"id":"chatcmpl-123","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":null}]}`)

	result, err := tmx.TranslateChunk(chunk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify it's valid JSON
	if !json.Valid(result) {
		t.Errorf("result is not valid JSON: %s", result)
	}
}

func TestOpenAIThinkTagTransition(t *testing.T) {
	tmx := NewOpenAITransmuxer()
	defer tmx.Close()

	// Simulate a <think> tag in content
	thinkChunk := []byte(`{"choices":[{"index":0,"delta":{"content":"<think>Let me reason about this"}}]}`)

	result, err := tmx.TranslateChunk(thinkChunk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should be transformed to reasoning_content
	if result != nil && len(result) > 0 {
		if !json.Valid(result) {
			t.Errorf("result is not valid JSON: %s", result)
		}
	}

	// Verify state transition
	if tmx.state != StateInsideThink {
		t.Error("expected state to be InsideThink after <think> tag")
	}
}

func TestOpenAIDeepSeekNativeReasoning(t *testing.T) {
	tmx := NewOpenAITransmuxer()
	defer tmx.Close()

	// DeepSeek native reasoning_content — should pass through
	chunk := []byte(`{"choices":[{"index":0,"delta":{"reasoning_content":"Thinking..."}}]}`)

	result, err := tmx.TranslateChunk(chunk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result == nil {
		t.Error("expected non-nil result for native reasoning_content")
	}
}

func TestAnthropicTextDelta(t *testing.T) {
	tmx := NewAnthropicTransmuxer()
	defer tmx.Close()

	// Set event type
	tmx.SetEventType("content_block_delta")

	// Anthropic text delta
	chunk := []byte(`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`)

	result, err := tmx.TranslateChunk(chunk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if !json.Valid(result) {
		t.Errorf("result is not valid JSON: %s", result)
	}

	// Verify it contains "content" field (not "reasoning_content")
	var parsed map[string]interface{}
	json.Unmarshal(result, &parsed)
	choices := parsed["choices"].([]interface{})
	delta := choices[0].(map[string]interface{})["delta"].(map[string]interface{})
	if _, ok := delta["content"]; !ok {
		t.Error("expected 'content' field in delta")
	}
}

func TestAnthropicThinkingDelta(t *testing.T) {
	tmx := NewAnthropicTransmuxer()
	defer tmx.Close()

	tmx.SetEventType("content_block_delta")

	chunk := []byte(`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"Let me think..."}}`)

	result, err := tmx.TranslateChunk(chunk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if !json.Valid(result) {
		t.Errorf("result is not valid JSON: %s", result)
	}

	// Verify it contains "reasoning_content" field
	var parsed map[string]interface{}
	json.Unmarshal(result, &parsed)
	choices := parsed["choices"].([]interface{})
	delta := choices[0].(map[string]interface{})["delta"].(map[string]interface{})
	if _, ok := delta["reasoning_content"]; !ok {
		t.Error("expected 'reasoning_content' field in delta")
	}
}

func TestAnthropicStopReasonMapping(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"end_turn", "stop"},
		{"max_tokens", "length"},
		{"stop_sequence", "stop"},
		{"tool_use", "tool_calls"},
	}

	for _, tt := range tests {
		result := mapAnthropicStopReason(tt.input)
		if result != tt.expected {
			t.Errorf("mapAnthropicStopReason(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestGeminiTextTranslation(t *testing.T) {
	tmx := NewGeminiTransmuxer()
	defer tmx.Close()

	chunk := []byte(`{"candidates":[{"content":{"parts":[{"text":"Hello world"}],"role":"model"}}]}`)

	result, err := tmx.TranslateChunk(chunk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if !json.Valid(result) {
		t.Errorf("result is not valid JSON: %s", result)
	}
}

func TestGeminiFinishReason(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"STOP", "stop"},
		{"MAX_TOKENS", "length"},
		{"SAFETY", "content_filter"},
	}

	for _, tt := range tests {
		result := mapGeminiFinishReason(tt.input)
		if result != tt.expected {
			t.Errorf("mapGeminiFinishReason(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func BenchmarkBuildDelta(b *testing.B) {
	content := []byte("Hello, this is a test response from the AI model.")

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		buildDelta(content, false, "")
	}
}

func BenchmarkOpenAIPassthrough(b *testing.B) {
	tmx := NewOpenAITransmuxer()
	chunk := []byte(`{"id":"chatcmpl-123","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":null}]}`)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		tmx.TranslateChunk(chunk)
	}
}

func BenchmarkAnthropicTranslation(b *testing.B) {
	tmx := NewAnthropicTransmuxer()
	tmx.SetEventType("content_block_delta")
	chunk := []byte(`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		tmx.TranslateChunk(chunk)
	}
}
