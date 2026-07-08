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

// isCloudflareImageRequest reports whether the incoming path targets the
// OpenAI image generations endpoint (e.g. /v1/images/generations).
func isCloudflareImageRequest(requestPath string) bool {
	return strings.Contains(requestPath, "/images/")
}
