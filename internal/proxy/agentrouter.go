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
//  2. OpenAI tools are translated to Anthropic tools (parameters -> input_schema).
//  3. Assistant tool_calls are converted to Anthropic tool_use content blocks.
//  4. User tool response messages (role: "tool") are converted to Anthropic tool_result blocks.
//  5. Reasoning content (reasoning_content / thinking) is preserved.
//  6. "max_tokens" is defaulted to 16384 if missing (Anthropic requires max_tokens).
//  7. Model prefix "agentrouter/" is stripped so AgentRouter receives the clean model name.
func TranslateOpenAIToAgentRouter(raw []byte) ([]byte, error) {
	if len(raw) == 0 {
		return raw, nil
	}

	modelStr, _ := jsonparser.GetString(raw, "model")
	cleanModel := strings.TrimPrefix(modelStr, "agentrouter/")

	var systemPrompts []string
	var anthropicMsgs []map[string]interface{}

	// Parse tools array if present
	var anthropicTools []map[string]interface{}
	toolsBytes, dtTools, _, _ := jsonparser.Get(raw, "tools")
	if dtTools == jsonparser.Array && len(toolsBytes) > 0 {
		var openAITools []struct {
			Type     string `json:"type"`
			Function struct {
				Name        string          `json:"name"`
				Description string          `json:"description,omitempty"`
				Parameters  json.RawMessage `json:"parameters,omitempty"`
			} `json:"function"`
		}
		if err := json.Unmarshal(toolsBytes, &openAITools); err == nil {
			for _, t := range openAITools {
				if t.Type == "function" && t.Function.Name != "" {
					toolObj := map[string]interface{}{
						"name": t.Function.Name,
					}
					if t.Function.Description != "" {
						toolObj["description"] = t.Function.Description
					}
					if len(t.Function.Parameters) > 0 {
						var schema interface{}
						if json.Unmarshal(t.Function.Parameters, &schema) == nil {
							toolObj["input_schema"] = schema
						} else {
							toolObj["input_schema"] = map[string]interface{}{"type": "object"}
						}
					} else {
						toolObj["input_schema"] = map[string]interface{}{"type": "object"}
					}
					anthropicTools = append(anthropicTools, toolObj)
				}
			}
		}
	}

	// Parse tool_choice if present
	var anthropicToolChoice interface{}
	tcBytes, dtChoice, _, _ := jsonparser.Get(raw, "tool_choice")
	if dtChoice == jsonparser.String {
		tcStr := string(tcBytes)
		if tcStr == "auto" {
			anthropicToolChoice = map[string]interface{}{"type": "auto"}
		} else if tcStr == "any" || tcStr == "required" {
			anthropicToolChoice = map[string]interface{}{"type": "any"}
		}
	} else if dtChoice == jsonparser.Object {
		var openAITC struct {
			Type     string `json:"type"`
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		}
		if json.Unmarshal(tcBytes, &openAITC) == nil && openAITC.Function.Name != "" {
			anthropicToolChoice = map[string]interface{}{
				"type": "tool",
				"name": openAITC.Function.Name,
			}
		}
	}

	// Parse messages array
	msgsBytes, dtMsgs, _, _ := jsonparser.Get(raw, "messages")
	if dtMsgs == jsonparser.Array {
		var openAIMsgs []struct {
			Role             string          `json:"role"`
			Content          json.RawMessage `json:"content"`
			ReasoningContent string          `json:"reasoning_content,omitempty"`
			ToolCallID       string          `json:"tool_call_id,omitempty"`
			ToolCalls        []struct {
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls,omitempty"`
		}

		if err := json.Unmarshal(msgsBytes, &openAIMsgs); err == nil {
			for _, msg := range openAIMsgs {
				// Handle system/developer roles
				if msg.Role == "system" || msg.Role == "developer" {
					sysText := parseRawContentToText(msg.Content)
					if sysText != "" {
						systemPrompts = append(systemPrompts, sysText)
					}
					continue
				}

				// Handle tool response message (role == "tool")
				if msg.Role == "tool" {
					toolResultContent := parseRawContentToText(msg.Content)
					toolResultBlock := map[string]interface{}{
						"type":        "tool_result",
						"tool_use_id": msg.ToolCallID,
						"content":     toolResultContent,
					}
					anthropicMsgs = append(anthropicMsgs, map[string]interface{}{
						"role":    "user",
						"content": []interface{}{toolResultBlock},
					})
					continue
				}

				// Map role
				anthropicRole := msg.Role
				if anthropicRole != "user" && anthropicRole != "assistant" {
					anthropicRole = "user"
				}

				// Build content blocks for assistant or user
				var blocks []interface{}

				// Reasoning content (if assistant)
				if msg.Role == "assistant" && msg.ReasoningContent != "" {
					blocks = append(blocks, map[string]interface{}{
						"type":     "thinking",
						"thinking": msg.ReasoningContent,
					})
				}

				// Text / array content
				parsedBlocks := parseRawContentToBlocks(msg.Content)
				if len(parsedBlocks) > 0 {
					blocks = append(blocks, parsedBlocks...)
				}

				// Tool calls (if assistant)
				if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
					for _, tc := range msg.ToolCalls {
						var argsMap interface{}
						if err := json.Unmarshal([]byte(tc.Function.Arguments), &argsMap); err != nil {
							argsMap = map[string]interface{}{}
						}
						blocks = append(blocks, map[string]interface{}{
							"type":  "tool_use",
							"id":    tc.ID,
							"name":  tc.Function.Name,
							"input": argsMap,
						})
					}
				}

				var anthropicContent interface{}
				if len(blocks) == 0 {
					if anthropicRole == "assistant" {
						anthropicContent = " "
					} else {
						continue // skip empty user messages
					}
				} else if len(blocks) == 1 {
					if textBlock, ok := blocks[0].(map[string]interface{}); ok && textBlock["type"] == "text" {
						anthropicContent = textBlock["text"]
					} else {
						anthropicContent = blocks
					}
				} else {
					anthropicContent = blocks
				}

				anthropicMsgs = append(anthropicMsgs, map[string]interface{}{
					"role":    anthropicRole,
					"content": anthropicContent,
				})
			}
		}
	}

	// Coalesce consecutive same-role messages
	anthropicMsgs = coalesceMessages(anthropicMsgs)

	maxTokens, _ := jsonparser.GetInt(raw, "max_tokens")
	if maxTokens <= 0 {
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

	if len(anthropicTools) > 0 {
		out["tools"] = anthropicTools
		if anthropicToolChoice != nil {
			out["tool_choice"] = anthropicToolChoice
		}
	}

	if temperature > 0 {
		out["temperature"] = temperature
	}

	if len(systemPrompts) > 0 {
		out["system"] = strings.Join(systemPrompts, "\n\n")
	}

	// Check for thinking options
	thinkingBytes, dtThink, _, _ := jsonparser.Get(raw, "thinking")
	if dtThink == jsonparser.Object {
		var thinkObj interface{}
		if json.Unmarshal(thinkingBytes, &thinkObj) == nil {
			out["thinking"] = thinkObj
		}
	}

	return json.Marshal(out)
}

// parseRawContentToText extracts plain text from OpenAI message content,
// handling both raw string and content block array formats.
func parseRawContentToText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var str string
	if json.Unmarshal(raw, &str) == nil {
		return str
	}
	var blocks []map[string]interface{}
	if json.Unmarshal(raw, &blocks) == nil {
		var parts []string
		for _, b := range blocks {
			if t, _ := b["type"].(string); t == "text" {
				if txt, ok := b["text"].(string); ok && txt != "" {
					parts = append(parts, txt)
				}
			}
		}
		return strings.Join(parts, "\n")
	}
	return string(raw)
}

// parseRawContentToBlocks converts raw message content into an array of content blocks.
func parseRawContentToBlocks(raw json.RawMessage) []interface{} {
	if len(raw) == 0 {
		return nil
	}
	var str string
	if json.Unmarshal(raw, &str) == nil {
		if str == "" {
			return nil
		}
		return []interface{}{map[string]interface{}{"type": "text", "text": str}}
	}
	var rawBlocks []interface{}
	if json.Unmarshal(raw, &rawBlocks) == nil {
		return rawBlocks
	}
	return nil
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
