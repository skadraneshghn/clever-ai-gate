package proxy

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/buger/jsonparser"
)

// ─── Request Translation (OpenAI → Cloudflare Workers AI) ───────────────────

// translateCloudflareImageRequest converts an OpenAI /v1/images/generations
// request body into the body expected by Cloudflare Workers AI text-to-image
// models served via the /ai/run/{model} endpoint.
//
//	OpenAI:     {"model":"@cf/...","prompt":"a cat","n":1,"size":"1024x1024","response_format":"b64_json"}
//	Cloudflare: {"prompt":"a cat"}
//
// Cloudflare's /ai/run text-to-image models reject unknown OpenAI fields
// (model, n, size, response_format, quality) with an "Invalid input"
// (code 8002) error, so we strip everything except the prompt.
//
// Third-party image models (openai/gpt-image-2, recraft/*, krea/*, etc.) served
// via Cloudflare AI Gateway accept the full OpenAI payload natively, so they do
// NOT need this translation — the handler detects this case and bypasses
// translation for third-party providers.
func translateCloudflareImageRequest(body []byte) ([]byte, string, error) {
	prompt, _ := jsonparser.GetString(body, "prompt")
	if prompt == "" {
		return nil, "", fmt.Errorf("missing 'prompt' field in cloudflare image generation request")
	}

	req := map[string]interface{}{
		"prompt": prompt,
	}

	out, err := json.Marshal(req)
	if err != nil {
		return nil, "", fmt.Errorf("failed to marshal cloudflare image request: %w", err)
	}
	return out, "application/json", nil
}

// ─── Response Translation (Cloudflare Workers AI → OpenAI) ──────────────────

// isBinaryImage reports whether the given byte slice starts with known
// image magic header bytes (PNG, JPEG, WebP, GIF, BMP).
func isBinaryImage(body []byte) bool {
	if len(body) < 8 {
		return false
	}
	// PNG: \x89PNG\r\n\x1a\n
	if bytes.HasPrefix(body, []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}) {
		return true
	}
	// JPEG: \xff\xd8\xff
	if bytes.HasPrefix(body, []byte{0xff, 0xd8, 0xff}) {
		return true
	}
	// WebP: RIFF....WEBP
	if len(body) >= 12 && bytes.HasPrefix(body, []byte("RIFF")) && bytes.Equal(body[8:12], []byte("WEBP")) {
		return true
	}
	// GIF: GIF87a or GIF89a
	if bytes.HasPrefix(body, []byte("GIF87a")) || bytes.HasPrefix(body, []byte("GIF89a")) {
		return true
	}
	// BMP: BM
	if bytes.HasPrefix(body, []byte("BM")) {
		return true
	}
	return false
}

// translateCloudflareImageResponse converts a Cloudflare Workers AI
// text-to-image response into an OpenAI /v1/images/generations response.
//
// Handles both formats returned by Cloudflare:
//  1. Raw binary image stream (e.g. image/png, image/jpeg bytes directly from /ai/run/{model})
//  2. JSON envelope: {"success":true,"result":{"image":"<base64>"}} or {"result":{"response":"<base64>"}}
//
// Converts either to OpenAI standard:
//
//	{"created": unix_timestamp, "data": [{"b64_json": "<base64_string>"}]}
func translateCloudflareImageResponse(body []byte) ([]byte, string, error) {
	if len(body) == 0 {
		return nil, "", fmt.Errorf("cloudflare image response was empty")
	}

	// 1. Raw binary image stream (PNG, JPEG, WebP, GIF, BMP)
	if isBinaryImage(body) {
		imageB64 := base64.StdEncoding.EncodeToString(body)
		resp := map[string]interface{}{
			"created": time.Now().Unix(),
			"data": []map[string]interface{}{
				{"b64_json": imageB64},
			},
		}
		out, err := json.Marshal(resp)
		if err != nil {
			return nil, "", fmt.Errorf("failed to marshal openai image response: %w", err)
		}
		return out, "application/json", nil
	}

	// 2. Cloudflare JSON envelope
	imageB64, _ := jsonparser.GetString(body, "result", "image")
	if imageB64 == "" {
		imageB64, _ = jsonparser.GetString(body, "result", "response")
	}
	if imageB64 == "" {
		imageB64, _ = jsonparser.GetString(body, "image")
	}
	if imageB64 == "" {
		return nil, "", fmt.Errorf("cloudflare image response did not contain an image")
	}

	resp := map[string]interface{}{
		"created": time.Now().Unix(),
		"data": []map[string]interface{}{
			{"b64_json": imageB64},
		},
	}

	out, err := json.Marshal(resp)
	if err != nil {
		return nil, "", fmt.Errorf("failed to marshal openai image response: %w", err)
	}
	return out, "application/json", nil
}

// ─── Detection helpers ───────────────────────────────────────────────────────

// isCloudflareImageRequest reports whether the incoming path targets the
// OpenAI image generations endpoint (e.g. /v1/images/generations).
func isCloudflareImageRequest(requestPath string) bool {
	return strings.Contains(requestPath, "/images/")
}

// isCloudflareNativeImageModel reports whether the model ID belongs to the
// native @cf/* Workers AI model namespace.
func isCloudflareNativeImageModel(modelID string) bool {
	return strings.HasPrefix(modelID, "@cf/")
}

// isCloudflareNativeImageResponse reports whether a response body is in the
// Cloudflare Workers AI native format (raw binary image OR {"success":...,"result":...}).
func isCloudflareNativeImageResponse(body []byte) bool {
	// Raw binary images are always native Cloudflare responses
	if isBinaryImage(body) {
		return true
	}
	// Presence of "success" key indicates native Cloudflare envelope
	_, dataType, _, err := jsonparser.Get(body, "success")
	if err == nil && dataType == jsonparser.Boolean {
		return true
	}
	return false
}

// isCloudflareModelAgreementError reports whether a Cloudflare error body indicates
// that the model requires submitting the "agree" prompt before inference.
func isCloudflareModelAgreementError(body []byte) bool {
	if len(body) == 0 {
		return false
	}
	bodyStr := string(body)
	return strings.Contains(bodyStr, "AiError: Model Agreement") ||
		(strings.Contains(bodyStr, "Model Agreement") && strings.Contains(bodyStr, "agree")) ||
		strings.Contains(bodyStr, "Prior to using this model, you must submit the prompt 'agree'")
}

// autoAcceptCloudflareModelAgreement automatically performs the required "agree"
// prompt handshake with Cloudflare Workers AI for Meta / restricted models.
func autoAcceptCloudflareModelAgreement(ctx context.Context, client *http.Client, accountID, apiToken, model string) error {
	if client == nil {
		client = http.DefaultClient
	}

	cleanModel := strings.TrimPrefix(model, "cloudflare/")

	// 1. Submit "agree" to the universal /ai/run/{model} endpoint
	runURL := fmt.Sprintf("https://api.cloudflare.com/client/v4/accounts/%s/ai/run/%s", accountID, cleanModel)
	runBody := []byte(`{"prompt":"agree"}`)

	runReq, err := http.NewRequestWithContext(ctx, http.MethodPost, runURL, bytes.NewReader(runBody))
	if err == nil {
		runReq.Header.Set("Authorization", "Bearer "+apiToken)
		runReq.Header.Set("Content-Type", "application/json")
		runReq.Header.Set("User-Agent", "CleverAIGate/1.0 (Agreement-Handshake)")

		if resp, doErr := client.Do(runReq); doErr == nil {
			io.Copy(io.Discard, resp.Body) //nolint:errcheck
			resp.Body.Close()
		}
	}

	// 2. Also submit "agree" to the OpenAI chat completions endpoint
	chatURL := fmt.Sprintf("https://api.cloudflare.com/client/v4/accounts/%s/ai/v1/chat/completions", accountID)
	chatBody := []byte(fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"agree"}]}`, cleanModel))

	chatReq, err := http.NewRequestWithContext(ctx, http.MethodPost, chatURL, bytes.NewReader(chatBody))
	if err == nil {
		chatReq.Header.Set("Authorization", "Bearer "+apiToken)
		chatReq.Header.Set("Content-Type", "application/json")
		chatReq.Header.Set("User-Agent", "CleverAIGate/1.0 (Agreement-Handshake)")

		if resp, doErr := client.Do(chatReq); doErr == nil {
			io.Copy(io.Discard, resp.Body) //nolint:errcheck
			resp.Body.Close()
		}
	}

	return nil
}

var cloudflareUnsupportedFields = []string{
	"stream_options",
	"store",
	"metadata",
	"service_tier",
	"parallel_tool_calls",
}

// sanitizeCloudflareRequest normalizes and sanitizes chat completion payloads
// destined for Cloudflare Workers AI (/ai/v1/chat/completions).
//
// Fixes:
//  1. Converts "role": "developer" to "role": "system".
//  2. In multimodal/vision requests (e.g. Llama 3.2 Vision), if a message contains
//     an image_url block but no non-empty text block, Cloudflare's internal tokenizer
//     fails with code 3030: "AiError: Unable to add image when there are no user-supplied
//     nor system-supplied messages." We ensure a non-empty text prompt exists.
//  3. Strips unsupported top-level OpenAI parameters (stream_options, store, metadata,
//     service_tier) that cause Cloudflare schema rejection.
//  4. Removes empty tool definitions ("tools": []).
func sanitizeCloudflareRequest(body []byte) []byte {
	if len(body) == 0 {
		return body
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return body
	}

	// 1. Strip "cloudflare/" routing prefix from model if present
	if modelRaw, ok := payload["model"].(string); ok {
		if strings.HasPrefix(modelRaw, "cloudflare/") {
			payload["model"] = strings.TrimPrefix(modelRaw, "cloudflare/")
		}
	}

	// 2. Strip unsupported top-level OpenAI parameters
	for _, f := range cloudflareUnsupportedFields {
		delete(payload, f)
	}

	// 3. Remove empty tools list
	if tools, ok := payload["tools"].([]interface{}); ok && len(tools) == 0 {
		delete(payload, "tools")
	}

	// 4. Normalize messages and ensure vision messages have valid text parts
	if messages, ok := payload["messages"].([]interface{}); ok {
		hasUserMsg := false
		for _, msgInterface := range messages {
			msgMap, isMap := msgInterface.(map[string]interface{})
			if !isMap {
				continue
			}

			// Normalize "developer" role -> "system"
			if role, ok := msgMap["role"].(string); ok {
				roleLower := strings.ToLower(role)
				if roleLower == "developer" {
					msgMap["role"] = "system"
				} else if roleLower == "user" {
					hasUserMsg = true
				}
			}

			// Replace null content with empty string
			if msgMap["content"] == nil {
				msgMap["content"] = ""
			}

			// Handle multimodal array content
			if parts, ok := msgMap["content"].([]interface{}); ok {
				hasImage := false
				hasText := false
				var textPart map[string]interface{}

				for _, partInterface := range parts {
					partMap, ok := partInterface.(map[string]interface{})
					if !ok {
						continue
					}
					pType, _ := partMap["type"].(string)
					if pType == "image_url" || pType == "image" {
						hasImage = true
					} else if pType == "text" {
						txt, _ := partMap["text"].(string)
						if strings.TrimSpace(txt) != "" {
							hasText = true
						}
						textPart = partMap
					}
				}

				// If message has image but NO non-empty text, add text prompt so Cloudflare's
				// Llama 3.2 Vision tokenizer does not throw code 3030:
				// "Unable to add image when there are no user-supplied nor system-supplied messages."
				if hasImage && !hasText {
					if textPart != nil {
						textPart["text"] = "Describe this image."
					} else {
						parts = append([]interface{}{
							map[string]interface{}{
								"type": "text",
								"text": "Describe this image.",
							},
						}, parts...)
						msgMap["content"] = parts
					}
				}
			}
		}

		// Ensure at least one user message exists if there are only system/assistant messages
		if !hasUserMsg && len(messages) > 0 {
			messages = append(messages, map[string]interface{}{
				"role":    "user",
				"content": "Describe this image.",
			})
			payload["messages"] = messages
		}
	}

	out, err := json.Marshal(payload)
	if err != nil {
		return body
	}
	return out
}

