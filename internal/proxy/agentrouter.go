package proxy

import (
	"bytes"
	"encoding/json"
)

// agentRouterUnsupportedFields lists standard OpenAI Chat Completions request fields
// that cause failures when AgentRouter proxies them to diverse upstream backends
// (DeepSeek, Claude, Llama, Mistral, etc.). These upstream models reject unrecognized
// fields with 400 Bad Request — the root cause of OmniRoute Issue #1921.
//
// The gateway strips these fields before forwarding to AgentRouter, keeping the
// request compatible with all possible upstream destinations. This is the same
// approach OmniRoute adopted after the bug was reported and fixed.
var agentRouterUnsupportedFields = []string{
	"store",               // OpenAI conversation storage — rejected by most upstreams
	"metadata",            // OpenAI request metadata — not part of standard chat spec
	"service_tier",        // OpenAI priority tier — vendor-specific
	"parallel_tool_calls", // OpenAI parallel function calling — not universally supported
	"logprobs",            // OpenAI log probabilities — not supported by many models
	"top_logprobs",        // OpenAI log probabilities — not supported by many models
	"logit_bias",          // OpenAI token bias — not universally supported
	"suffix",              // OpenAI completion suffix — legacy
}

// sanitizeAgentRouterRequest cleans and normalizes an OpenAI-compatible request body
// before forwarding to AgentRouter. This implements the OmniRoute Issue #1921 fix.
//
// Sanitization rules:
//
//  1. Strip unsupported top-level fields that upstream models reject with 400.
//  2. Strip "stream_options" if "stream" is not true — tools often send stream_options
//     even for non-streaming requests, causing upstream rejections.
//  3. Normalize "developer" role messages to "system" — the newer OpenAI "developer"
//     role is rejected by many AgentRouter upstream models (DeepSeek, Llama, etc.).
//  4. Strip "reasoning_effort" — only supported by specific OpenAI models, rejected
//     by most AgentRouter upstream destinations.
//
// The function uses a fast bytes.Contains probe to skip processing entirely when
// no unsupported field is present, keeping the common clean-request path allocation-free.
func sanitizeAgentRouterRequest(body []byte) []byte {
	if len(body) == 0 {
		return body
	}

	// Fast probe: check if ANY sanitization is needed before doing any work.
	needsFieldStrip := false
	for _, f := range agentRouterUnsupportedFields {
		if bytes.Contains(body, []byte(`"`+f+`"`)) {
			needsFieldStrip = true
			break
		}
	}

	needsStreamFix := bytes.Contains(body, []byte(`"stream_options"`))
	needsRoleFix := bytes.Contains(body, []byte(`"developer"`))
	needsReasoningFix := bytes.Contains(body, []byte(`"reasoning_effort"`))

	if !needsFieldStrip && !needsStreamFix && !needsRoleFix && !needsReasoningFix {
		return body // Fast path: no sanitization needed
	}

	// For role normalization, stream_options conditional logic, and reasoning_effort
	// stripping, we need to parse the JSON. Field stripping can be done with
	// jsonparser.Delete but since we may need to Marshal anyway for role changes,
	// we use a unified json.Unmarshal approach when complex changes are needed.
	var rawMap map[string]interface{}
	if err := json.Unmarshal(body, &rawMap); err != nil {
		return body // Malformed JSON — let upstream reject it normally
	}

	modified := false

	// 1. Strip unsupported top-level fields.
	for _, f := range agentRouterUnsupportedFields {
		if _, exists := rawMap[f]; exists {
			delete(rawMap, f)
			modified = true
		}
	}

	// 2. Strip stream_options if stream is not true.
	// Tools like Kilo/Cursor sometimes send stream_options even on non-streaming
	// requests, which upstream models reject.
	if _, hasStreamOpts := rawMap["stream_options"]; hasStreamOpts {
		stream, isStreamBool := rawMap["stream"].(bool)
		if !isStreamBool || !stream {
			delete(rawMap, "stream_options")
			modified = true
		}
	}

	// 3. Strip reasoning_effort — only specific OpenAI o-series models support it.
	// AgentRouter routes to many upstreams that reject this field.
	if _, has := rawMap["reasoning_effort"]; has {
		delete(rawMap, "reasoning_effort")
		modified = true
	}

	// 4. Normalize "developer" role messages to "system".
	// The "developer" role was introduced in newer OpenAI API versions but is
	// rejected by most AgentRouter upstream models (DeepSeek, Claude, Llama, etc.).
	if messages, ok := rawMap["messages"].([]interface{}); ok {
		for _, msg := range messages {
			if msgMap, ok := msg.(map[string]interface{}); ok {
				if role, ok := msgMap["role"].(string); ok && role == "developer" {
					msgMap["role"] = "system"
					modified = true
				}
			}
		}
	}

	if !modified {
		return body
	}

	sanitized, err := json.Marshal(rawMap)
	if err != nil {
		return body // Marshal failed — return original
	}
	return sanitized
}
