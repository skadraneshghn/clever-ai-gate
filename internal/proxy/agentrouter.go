package proxy

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/buger/jsonparser"
)

// TranslateOpenAIToAgentRouter converts an incoming OpenAI-style /v1/chat/completions
// request body into Anthropic's /v1/messages format required by AgentRouter.org.
//
// Key translations:
//  1. System and developer role messages are combined into Anthropic's top-level "system" prompt.
//  2. User and assistant messages are mapped into Anthropic's "messages" array.
//  3. "max_tokens" is defaulted to 4096 if missing (Anthropic requires max_tokens).
//  4. Model prefix "agentrouter/" is stripped so AgentRouter receives the clean model name.
func TranslateOpenAIToAgentRouter(raw []byte) ([]byte, error) {
	if len(raw) == 0 {
		return raw, nil
	}

	modelStr, _ := jsonparser.GetString(raw, "model")
	cleanModel := strings.TrimPrefix(modelStr, "agentrouter/")

	var systemPrompts []string
	var anthropicMsgs []map[string]interface{}

	// Parse messages array
	_, err := jsonparser.ArrayEach(raw, func(value []byte, dataType jsonparser.ValueType, offset int, err error) {
		role, _ := jsonparser.GetString(value, "role")

		// Handle content — can be string, array, or null
		contentBytes, dt, _, _ := jsonparser.Get(value, "content")
		var contentVal interface{}
		if dt == jsonparser.Null || len(contentBytes) == 0 {
			// null or missing content (e.g. assistant messages with only tool_calls)
			contentVal = ""
		} else if len(contentBytes) > 0 && contentBytes[0] == '[' {
			var rawArr []interface{}
			if err := json.Unmarshal(contentBytes, &rawArr); err == nil {
				contentVal = rawArr
			} else {
				contentVal = string(contentBytes)
			}
		} else {
			strVal, _ := jsonparser.GetString(value, "content")
			contentVal = strVal
		}

		if role == "system" || role == "developer" {
			// Extract text from system messages regardless of content format
			sysText := extractTextFromContent(contentVal)
			if sysText != "" {
				systemPrompts = append(systemPrompts, sysText)
			}
			return
		}

		// Map OpenAI roles to Anthropic roles
		anthropicRole := role
		if anthropicRole == "tool" {
			// tool results in OpenAI become user messages in Anthropic
			anthropicRole = "user"
		}
		if anthropicRole != "user" && anthropicRole != "assistant" {
			anthropicRole = "user"
		}

		// Skip empty content for non-assistant messages
		if contentVal == "" && anthropicRole != "assistant" {
			return
		}

		// Build Anthropic content — ensure it's always a proper content block
		var anthropicContent interface{}
		switch v := contentVal.(type) {
		case string:
			if v == "" {
				// Empty assistant message (e.g. before tool_calls) — use minimal text
				anthropicContent = " "
			} else {
				anthropicContent = v
			}
		case []interface{}:
			anthropicContent = v
		default:
			anthropicContent = fmt.Sprintf("%v", contentVal)
		}

		anthropicMsgs = append(anthropicMsgs, map[string]interface{}{
			"role":    anthropicRole,
			"content": anthropicContent,
		})
	}, "messages")

	if err != nil {
		return raw, fmt.Errorf("failed to parse messages for agentrouter translation: %w", err)
	}

	// Coalesce consecutive same-role messages — Anthropic API requires strict
	// user/assistant alternation. Kilo/Cursor/Cline commonly send multiple
	// consecutive user messages (project context, file contents, question).
	anthropicMsgs = coalesceMessages(anthropicMsgs)

	maxTokens, _ := jsonparser.GetInt(raw, "max_tokens")
	if maxTokens <= 0 {
		// Coding assistants need larger output buffers than the Anthropic default
		maxTokens = 16384
	}

	stream, _ := jsonparser.GetBoolean(raw, "stream")
	temperature, _ := jsonparser.GetFloat(raw, "temperature")

	out := map[string]interface{}{
		"model":      cleanModel,
		"messages":   anthropicMsgs,
		"max_tokens": maxTokens,
		"stream":     stream,
	}

	if temperature > 0 {
		out["temperature"] = temperature
	}

	if len(systemPrompts) > 0 {
		out["system"] = strings.Join(systemPrompts, "\n\n")
	}

	return json.Marshal(out)
}

// extractTextFromContent extracts plain text from OpenAI content values,
// handling both string and array-of-content-block formats.
func extractTextFromContent(contentVal interface{}) string {
	switch v := contentVal.(type) {
	case string:
		return v
	case []interface{}:
		var parts []string
		for _, item := range v {
			if block, ok := item.(map[string]interface{}); ok {
				if t, _ := block["type"].(string); t == "text" {
					if text, ok := block["text"].(string); ok && text != "" {
						parts = append(parts, text)
					}
				}
			}
		}
		return strings.Join(parts, "\n")
	default:
		return fmt.Sprintf("%v", contentVal)
	}
}

// coalesceMessages merges consecutive messages with the same role into a single
// message, as required by Anthropic's Messages API (strict user/assistant alternation).
// Content from multiple messages is combined into a single content array.
func coalesceMessages(msgs []map[string]interface{}) []map[string]interface{} {
	if len(msgs) <= 1 {
		return msgs
	}

	var result []map[string]interface{}
	for _, msg := range msgs {
		role, _ := msg["role"].(string)
		content := msg["content"]

		if len(result) > 0 {
			lastRole, _ := result[len(result)-1]["role"].(string)
			if lastRole == role {
				// Same role — merge content into previous message
				result[len(result)-1]["content"] = mergeContent(
					result[len(result)-1]["content"], content,
				)
				continue
			}
		}

		result = append(result, msg)
	}

	return result
}

// mergeContent combines two Anthropic content values into a single content array.
// Handles string and []interface{} content types.
func mergeContent(existing, incoming interface{}) interface{} {
	toBlocks := func(v interface{}) []interface{} {
		switch c := v.(type) {
		case string:
			if c == "" || c == " " {
				return nil
			}
			return []interface{}{map[string]interface{}{"type": "text", "text": c}}
		case []interface{}:
			return c
		default:
			s := fmt.Sprintf("%v", v)
			if s == "" {
				return nil
			}
			return []interface{}{map[string]interface{}{"type": "text", "text": s}}
		}
	}

	blocks := toBlocks(existing)
	blocks = append(blocks, toBlocks(incoming)...)

	if len(blocks) == 0 {
		return " "
	}
	return blocks
}

// translateAgentRouterResponseToOpenAI converts Anthropic /v1/messages non-streaming
// JSON response from AgentRouter into an OpenAI /v1/chat/completions JSON payload.
//
// Supports text content, thinking/reasoning blocks (for DeepSeek R1 / Claude thinking),
// tool calls, and token usage.
func translateAgentRouterResponseToOpenAI(raw []byte, requestedModel string) ([]byte, bool) {
	if len(raw) == 0 {
		return nil, false
	}

	// Unmarshal Anthropic response structure
	type anthropicBlock struct {
		Type     string          `json:"type"`
		Text     string          `json:"text,omitempty"`
		Thinking string          `json:"thinking,omitempty"`
		ID       string          `json:"id,omitempty"`
		Name     string          `json:"name,omitempty"`
		Input    json.RawMessage `json:"input,omitempty"`
	}

	type anthropicUsage struct {
		InputTokens  int64 `json:"input_tokens"`
		OutputTokens int64 `json:"output_tokens"`
	}

	type anthropicResp struct {
		ID         string           `json:"id"`
		Type       string           `json:"type"`
		Role       string           `json:"role"`
		Model      string           `json:"model"`
		Content    []anthropicBlock `json:"content"`
		StopReason *string          `json:"stop_reason"`
		Usage      anthropicUsage   `json:"usage"`
	}

	var antResp anthropicResp
	if err := json.Unmarshal(raw, &antResp); err != nil || antResp.Type != "message" {
		return nil, false // Not an Anthropic message response — return false for passthrough
	}

	modelName := antResp.Model
	if modelName == "" {
		modelName = requestedModel
	}

	msgID := antResp.ID
	if msgID == "" {
		msgID = fmt.Sprintf("chatcmpl-agentrouter-%d", time.Now().Unix())
	} else if !strings.HasPrefix(msgID, "chatcmpl-") {
		msgID = "chatcmpl-" + msgID
	}

	var textContent strings.Builder
	var reasoningContent strings.Builder

	type openAIFunction struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	}

	type openAIToolCall struct {
		ID       string         `json:"id"`
		Type     string         `json:"type"`
		Function openAIFunction `json:"function"`
	}

	var toolCalls []openAIToolCall

	for _, block := range antResp.Content {
		switch block.Type {
		case "text":
			textContent.WriteString(block.Text)
		case "thinking":
			reasoningContent.WriteString(block.Thinking)
		case "tool_use":
			argsStr := string(block.Input)
			if argsStr == "" {
				argsStr = "{}"
			}
			toolCalls = append(toolCalls, openAIToolCall{
				ID:   block.ID,
				Type: "function",
				Function: openAIFunction{
					Name:      block.Name,
					Arguments: argsStr,
				},
			})
		}
	}

	finishReason := "stop"
	if antResp.StopReason != nil {
		switch *antResp.StopReason {
		case "end_turn", "stop_sequence":
			finishReason = "stop"
		case "max_tokens":
			finishReason = "length"
		case "tool_use":
			finishReason = "tool_calls"
		default:
			finishReason = *antResp.StopReason
		}
	}

	type openAIMessage struct {
		Role             string           `json:"role"`
		Content          string           `json:"content"`
		ReasoningContent string           `json:"reasoning_content,omitempty"`
		ToolCalls        []openAIToolCall `json:"tool_calls,omitempty"`
	}

	type openAIChoice struct {
		Index        int           `json:"index"`
		Message      openAIMessage `json:"message"`
		FinishReason string        `json:"finish_reason"`
	}

	type openAIUsage struct {
		PromptTokens     int64 `json:"prompt_tokens"`
		CompletionTokens int64 `json:"completion_tokens"`
		TotalTokens      int64 `json:"total_tokens"`
	}

	type openAIResp struct {
		ID      string         `json:"id"`
		Object  string         `json:"object"`
		Created int64          `json:"created"`
		Model   string         `json:"model"`
		Choices []openAIChoice `json:"choices"`
		Usage   openAIUsage    `json:"usage"`
	}

	resp := openAIResp{
		ID:      msgID,
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   modelName,
		Choices: []openAIChoice{
			{
				Index: 0,
				Message: openAIMessage{
					Role:             "assistant",
					Content:          textContent.String(),
					ReasoningContent: reasoningContent.String(),
					ToolCalls:        toolCalls,
				},
				FinishReason: finishReason,
			},
		},
		Usage: openAIUsage{
			PromptTokens:     antResp.Usage.InputTokens,
			CompletionTokens: antResp.Usage.OutputTokens,
			TotalTokens:      antResp.Usage.InputTokens + antResp.Usage.OutputTokens,
		},
	}

	out, err := json.Marshal(resp)
	if err != nil {
		return nil, false
	}
	return out, true
}
