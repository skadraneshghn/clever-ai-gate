package transmux

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAnthropicTransmuxerThinkingDelta(t *testing.T) {
	tmx := NewAnthropicTransmuxer()
	defer tmx.Close()

	tmx.SetEventType("content_block_start")
	chunk, err := tmx.TranslateChunk([]byte(`{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tmx.SetEventType("content_block_delta")
	chunk, err = tmx.TranslateChunk([]byte(`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"Let us analyze the repo"}}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(chunk) == 0 {
		t.Fatalf("expected non-empty chunk for thinking_delta")
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(chunk, &parsed); err != nil {
		t.Fatalf("failed to parse chunk json: %v", err)
	}

	choices := parsed["choices"].([]interface{})
	choice0 := choices[0].(map[string]interface{})
	delta := choice0["delta"].(map[string]interface{})

	if delta["reasoning_content"] != "Let us analyze the repo" {
		t.Errorf("expected reasoning_content 'Let us analyze the repo', got '%v'", delta["reasoning_content"])
	}
}

func TestAnthropicTransmuxerToolUseStreaming(t *testing.T) {
	tmx := NewAnthropicTransmuxer()
	defer tmx.Close()

	// 1. message_start
	tmx.SetEventType("message_start")
	_, _ = tmx.TranslateChunk([]byte(`{"type":"message_start","message":{"id":"msg_123","model":"claude-3-5-sonnet"}}`))

	// 2. content_block_start for tool_use
	tmx.SetEventType("content_block_start")
	startChunk, err := tmx.TranslateChunk([]byte(`{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_01ABC","name":"list_dir"}}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var startParsed map[string]interface{}
	if err := json.Unmarshal(startChunk, &startParsed); err != nil {
		t.Fatalf("failed to parse start chunk: %v", err)
	}

	delta0 := startParsed["choices"].([]interface{})[0].(map[string]interface{})["delta"].(map[string]interface{})
	toolCalls0 := delta0["tool_calls"].([]interface{})
	tool0 := toolCalls0[0].(map[string]interface{})

	if tool0["id"] != "toolu_01ABC" {
		t.Errorf("expected tool id 'toolu_01ABC', got '%v'", tool0["id"])
	}
	fn0 := tool0["function"].(map[string]interface{})
	if fn0["name"] != "list_dir" {
		t.Errorf("expected tool name 'list_dir', got '%v'", fn0["name"])
	}

	// 3. content_block_delta with input_json_delta
	tmx.SetEventType("content_block_delta")
	inputChunk, err := tmx.TranslateChunk([]byte(`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"path\":\"/src\"}"}}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var inputParsed map[string]interface{}
	if err := json.Unmarshal(inputChunk, &inputParsed); err != nil {
		t.Fatalf("failed to parse input chunk: %v", err)
	}

	delta1 := inputParsed["choices"].([]interface{})[0].(map[string]interface{})["delta"].(map[string]interface{})
	toolCalls1 := delta1["tool_calls"].([]interface{})
	fn1 := toolCalls1[0].(map[string]interface{})["function"].(map[string]interface{})

	if fn1["arguments"] != "{\"path\":\"/src\"}" {
		t.Errorf("expected arguments '{\"path\":\"/src\"}', got '%v'", fn1["arguments"])
	}

	// 4. content_block_stop
	tmx.SetEventType("content_block_stop")
	_, _ = tmx.TranslateChunk([]byte(`{"type":"content_block_stop","index":0}`))

	// 5. message_delta with stop_reason tool_use
	tmx.SetEventType("message_delta")
	stopChunk, err := tmx.TranslateChunk([]byte(`{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"input_tokens":10,"output_tokens":20}}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var stopParsed map[string]interface{}
	if err := json.Unmarshal(stopChunk, &stopParsed); err != nil {
		t.Fatalf("failed to parse stop chunk: %v", err)
	}

	finishReason := stopParsed["choices"].([]interface{})[0].(map[string]interface{})["finish_reason"]
	if finishReason != "tool_calls" {
		t.Errorf("expected finish_reason 'tool_calls', got '%v'", finishReason)
	}
}

func TestAnthropicTransmuxerToolUseMissingID(t *testing.T) {
	// GPT models proxied through agentrouter may omit content_block.id in the
	// Anthropic SSE for tool_use blocks. The transmuxer must generate a stable,
	// non-empty ID so the client can correctly pair tool results on the next turn.
	tmx := NewAnthropicTransmuxer()
	defer tmx.Close()

	// 1. message_start
	tmx.SetEventType("message_start")
	_, _ = tmx.TranslateChunk([]byte(`{"type":"message_start","message":{"id":"msg_123","model":"gpt-5.6-sol"}}`))

	// 2. content_block_start for tool_use WITHOUT an id field
	tmx.SetEventType("content_block_start")
	startChunk, err := tmx.TranslateChunk([]byte(`{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","name":"read_file"}}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var startParsed map[string]interface{}
	if err := json.Unmarshal(startChunk, &startParsed); err != nil {
		t.Fatalf("failed to parse start chunk: %v", err)
	}

	delta := startParsed["choices"].([]interface{})[0].(map[string]interface{})["delta"].(map[string]interface{})
	toolCalls := delta["tool_calls"].([]interface{})
	tool0 := toolCalls[0].(map[string]interface{})

	id, _ := tool0["id"].(string)
	if id == "" {
		t.Errorf("expected non-empty generated tool id when content_block.id is missing")
	}
	if !strings.HasPrefix(id, "call_gate_") {
		t.Errorf("expected stable 'call_gate_' prefixed id, got %q", id)
	}

	fn := tool0["function"].(map[string]interface{})
	if fn["name"] != "read_file" {
		t.Errorf("expected tool name 'read_file', got %v", fn["name"])
	}

	// 3. content_block_delta with input_json_delta — should use same toolIndex
	tmx.SetEventType("content_block_delta")
	inputChunk, err := tmx.TranslateChunk([]byte(`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"path\":\"/x\"}"}}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var inputParsed map[string]interface{}
	if err := json.Unmarshal(inputChunk, &inputParsed); err != nil {
		t.Fatalf("failed to parse input chunk: %v", err)
	}

	delta1 := inputParsed["choices"].([]interface{})[0].(map[string]interface{})["delta"].(map[string]interface{})
	toolCalls1 := delta1["tool_calls"].([]interface{})
	toolIdx := toolCalls1[0].(map[string]interface{})["index"]
	if toolIdx != float64(0) {
		t.Errorf("expected tool index 0, got %v", toolIdx)
	}
}

func TestAnthropicTransmuxerToolUseMissingName(t *testing.T) {
	// AgentRouter's GPT-to-Anthropic conversion sometimes omits both id and name
	// from content_block_start for tool_use. The transmuxer must generate fallbacks
	// for both so the client receives a valid OpenAI tool call.
	tmx := NewAnthropicTransmuxer()
	defer tmx.Close()

	tmx.SetEventType("message_start")
	_, _ = tmx.TranslateChunk([]byte(`{"type":"message_start","message":{"id":"msg_123","model":"gpt-5.6-sol"}}`))

	// content_block_start for tool_use WITHOUT id or name
	tmx.SetEventType("content_block_start")
	startChunk, err := tmx.TranslateChunk([]byte(`{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","input":{}}}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(startChunk, &parsed); err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	delta := parsed["choices"].([]interface{})[0].(map[string]interface{})["delta"].(map[string]interface{})
	toolCalls := delta["tool_calls"].([]interface{})
	tool0 := toolCalls[0].(map[string]interface{})

	id, _ := tool0["id"].(string)
	if id == "" {
		t.Errorf("expected non-empty generated tool id")
	}
	if !strings.HasPrefix(id, "call_gate_") {
		t.Errorf("expected 'call_gate_' prefix, got %q", id)
	}

	fn := tool0["function"].(map[string]interface{})
	name, _ := fn["name"].(string)
	if name == "" {
		t.Errorf("expected non-empty fallback tool name")
	}
	if name != "unknown_tool" {
		t.Errorf("expected fallback name 'unknown_tool', got %q", name)
	}
}
