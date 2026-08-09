package proxy

import (
	"encoding/json"
	"testing"
)

func TestTranslateOpenAIToAgentRouter(t *testing.T) {
	openAIRaw := []byte(`{
		"model": "agentrouter/claude-3-5-sonnet-20241022",
		"messages": [
			{"role": "system", "content": "You are a helpful coding assistant."},
			{"role": "user", "content": "Hello, write a hello world in Go"}
		],
		"temperature": 0.7,
		"stream": true
	}`)

	agentBody, err := TranslateOpenAIToAgentRouter(openAIRaw)
	if err != nil {
		t.Fatalf("unexpected error during translation: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(agentBody, &parsed); err != nil {
		t.Fatalf("failed to parse translated json: %v", err)
	}

	if parsed["model"] != "claude-3-5-sonnet-20241022" {
		t.Errorf("expected clean model name 'claude-3-5-sonnet-20241022', got '%v'", parsed["model"])
	}

	if parsed["system"] != "You are a helpful coding assistant." {
		t.Errorf("expected system prompt 'You are a helpful coding assistant.', got '%v'", parsed["system"])
	}

	if parsed["stream"] != true {
		t.Errorf("expected stream to be true, got %v", parsed["stream"])
	}

	msgs, ok := parsed["messages"].([]interface{})
	if !ok || len(msgs) != 1 {
		t.Fatalf("expected 1 user message, got %d", len(msgs))
	}
}

func TestTranslateAgentRouterResponseToOpenAI(t *testing.T) {
	anthropicRaw := []byte(`{
		"id": "msg_013Z55f4k5g6h7j8",
		"type": "message",
		"role": "assistant",
		"model": "claude-3-5-sonnet-20241022",
		"content": [
			{"type": "thinking", "thinking": "Let us solve this problem step by step..."},
			{"type": "text", "text": "Here is the code in Go:\nfunc main() {}"}
		],
		"stop_reason": "end_turn",
		"usage": {
			"input_tokens": 15,
			"output_tokens": 42
		}
	}`)

	openAIPayload, ok := translateAgentRouterResponseToOpenAI(anthropicRaw, "claude-3-5-sonnet-20241022")
	if !ok {
		t.Fatalf("expected successful translation of Anthropic non-streaming response")
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(openAIPayload, &parsed); err != nil {
		t.Fatalf("failed to parse output json: %v", err)
	}

	if parsed["object"] != "chat.completion" {
		t.Errorf("expected object 'chat.completion', got '%v'", parsed["object"])
	}

	choices := parsed["choices"].([]interface{})
	choice0 := choices[0].(map[string]interface{})
	msg := choice0["message"].(map[string]interface{})

	if msg["content"] != "Here is the code in Go:\nfunc main() {}" {
		t.Errorf("unexpected content: %v", msg["content"])
	}

	if msg["reasoning_content"] != "Let us solve this problem step by step..." {
		t.Errorf("unexpected reasoning_content: %v", msg["reasoning_content"])
	}

	if choice0["finish_reason"] != "stop" {
		t.Errorf("expected finish_reason 'stop', got '%v'", choice0["finish_reason"])
	}

	usage := parsed["usage"].(map[string]interface{})
	if usage["prompt_tokens"].(float64) != 15 || usage["completion_tokens"].(float64) != 42 {
		t.Errorf("unexpected usage numbers: %v", usage)
	}
}
