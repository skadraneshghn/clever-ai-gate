package proxy

import (
	"encoding/json"
	"testing"
)

func TestSanitizePuterRequest(t *testing.T) {
	// 1. A request missing "content" key in one message
	body := []byte(`{
		"model": "gpt-4o",
		"messages": [
			{"role": "user", "content": "hello"},
			{"role": "assistant", "tool_calls": []}
		]
	}`)

	sanitized := sanitizePuterRequest(body)

	var req map[string]interface{}
	if err := json.Unmarshal(sanitized, &req); err != nil {
		t.Fatalf("failed to unmarshal sanitized JSON: %v", err)
	}

	messages, ok := req["messages"].([]interface{})
	if !ok || len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %+v", req["messages"])
	}

	m2, ok := messages[1].(map[string]interface{})
	if !ok {
		t.Fatal("expected second message to be an object")
	}

	content, exists := m2["content"]
	if !exists {
		t.Error("expected 'content' field to be injected")
	}
	if content != "" {
		t.Errorf("expected 'content' to be empty string, got %v", content)
	}

	// 2. A request where "content" is null
	bodyNull := []byte(`{
		"messages": [
			{"role": "user", "content": null}
		]
	}`)

	sanitizedNull := sanitizePuterRequest(bodyNull)

	var reqNull map[string]interface{}
	json.Unmarshal(sanitizedNull, &reqNull)

	mNull := reqNull["messages"].([]interface{})[0].(map[string]interface{})
	if mNull["content"] != "" {
		t.Errorf("expected 'content' to be empty string, got %v", mNull["content"])
	}
}
