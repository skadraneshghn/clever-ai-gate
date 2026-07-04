package proxy

import (
	"encoding/json"
)

// sanitizePuterRequest ensures that every message in the "messages" array
// has a non-nil "content" property. Puter's upstream proxy strictly rejects
// requests with HTTP 400 if any message (e.g., tool execution/response blocks)
// lacks a "content" string.
func sanitizePuterRequest(body []byte) []byte {
	if len(body) == 0 {
		return body
	}

	var req map[string]interface{}
	if err := json.Unmarshal(body, &req); err != nil {
		return body // malformed json, let upstream reject it normally
	}

	messagesVal, ok := req["messages"]
	if !ok {
		return body
	}

	messages, ok := messagesVal.([]interface{})
	if !ok {
		return body
	}

	modified := false
	for _, msgVal := range messages {
		msg, ok := msgVal.(map[string]interface{})
		if !ok {
			continue
		}

		contentVal, exists := msg["content"]
		if !exists || contentVal == nil {
			msg["content"] = ""
			modified = true
		}
	}

	if !modified {
		return body
	}

	if sanitized, err := json.Marshal(req); err == nil {
		return sanitized
	}

	return body
}
