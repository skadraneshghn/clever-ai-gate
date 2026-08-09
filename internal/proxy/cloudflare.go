package proxy

import (
	"encoding/json"
	"fmt"
	"strings"

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

// translateCloudflareImageResponse converts a Cloudflare Workers AI
// text-to-image response into an OpenAI /v1/images/generations response.
//
//	Cloudflare: {"success":true,"result":{"image":"<base64 image bytes>"}}
//	OpenAI:     {"created":0,"data":[{"b64_json":"<base64>"}]}
//
// The base64 string returned by Cloudflare is a raw base64-encoded image (not a
// data URI), which maps directly onto OpenAI's b64_json field.
//
// Third-party image models (openai/gpt-image-2, recraft/*, krea/*, etc.) return
// an OpenAI-native response directly — use isCloudflareNativeImageResponse() to
// detect which format the upstream returned before calling this function.
func translateCloudflareImageResponse(body []byte) ([]byte, string, error) {
	imageB64, _ := jsonparser.GetString(body, "result", "image")
	if imageB64 == "" {
		// Some Cloudflare image models nest the image under result.response.
		imageB64, _ = jsonparser.GetString(body, "result", "response")
	}
	if imageB64 == "" {
		return nil, "", fmt.Errorf("cloudflare image response did not contain an image")
	}

	resp := map[string]interface{}{
		"created": 0,
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
//
// Native @cf/* models return a Cloudflare-specific envelope:
//
//	{"success": true, "result": {"image": "<base64>"}}
//
// Third-party models (openai/*, anthropic/*, google/*, recraft/*, krea/*, etc.)
// are proxied through Cloudflare AI Gateway and return the OpenAI-native format
// directly:
//
//	{"created": 1234567890, "data": [{"b64_json": "<base64>"}]}
//
// This function is used by the handler to decide whether to:
//   - Run request/response translation (native @cf/* models)
//   - Pass through the request/response unchanged (third-party models)
func isCloudflareNativeImageModel(modelID string) bool {
	return strings.HasPrefix(modelID, "@cf/")
}

// isCloudflareNativeImageResponse reports whether a response body is in the
// Cloudflare Workers AI native envelope format ({"success":...,"result":...}).
//
// This provides a secondary detection path when the model ID alone is ambiguous.
// If the response has a top-level "success" boolean field, it is a native CF envelope.
// If it has a top-level "data" array, it is already in OpenAI format.
func isCloudflareNativeImageResponse(body []byte) bool {
	// Presence of "success" key indicates native Cloudflare envelope
	_, dataType, _, err := jsonparser.Get(body, "success")
	if err == nil && dataType == jsonparser.Boolean {
		return true
	}
	return false
}
