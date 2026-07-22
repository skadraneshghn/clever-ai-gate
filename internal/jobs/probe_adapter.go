package jobs

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// ModelProbeRequest encapsulates the targeted HTTP endpoint, method, headers, and body for model probing.
type ModelProbeRequest struct {
	Method  string
	URL     string
	Headers map[string]string
	Body    []byte
}

// CleanUpstreamModel strips the routing pool provider prefix (e.g. nvidia/, cloudflare/, ollama/, 1min/)
// from a model pattern to produce the exact model identifier expected by the upstream provider.
func CleanUpstreamModel(modelPattern, providerID string) string {
	model := modelPattern
	provider := strings.ToLower(providerID)

	// If model starts with provider + "/", strip it (e.g. nvidia/google/gemma-2-2b-it -> google/gemma-2-2b-it)
	if strings.HasPrefix(strings.ToLower(model), provider+"/") {
		return model[len(provider)+1:]
	}

	// Handle specific provider prefix aliases
	switch provider {
	case "nvidia":
		model = strings.TrimPrefix(model, "nvidia/")
	case "ollama":
		model = strings.TrimPrefix(model, "ollama/")
	case "cloudflare":
		model = strings.TrimPrefix(model, "cloudflare/")
	case "huggingface":
		model = strings.TrimPrefix(model, "huggingface/")
	case "openrouter":
		model = strings.TrimPrefix(model, "openrouter/")
	case "deepinfra":
		model = strings.TrimPrefix(model, "deepinfra/")
	case "1minai", "1min":
		model = strings.TrimPrefix(model, "1min/")
	case "freemodel", "freemodel-cc":
		model = strings.TrimPrefix(model, "freemodel/")
		model = strings.TrimPrefix(model, "freemodel-cc/")
	case "gemini":
		model = strings.TrimPrefix(model, "gemini/")
	}

	return model
}

// BuildAdaptiveProbeRequest constructs a capability- and provider-aware HTTP probe request.
func BuildAdaptiveProbeRequest(baseURL, apiKey, providerID, modelPattern string, capabilities map[string]bool) (*ModelProbeRequest, error) {
	cleanBaseURL := strings.TrimRight(baseURL, "/")
	provider := strings.ToLower(providerID)
	modelLower := strings.ToLower(modelPattern)

	targetModel := CleanUpstreamModel(modelPattern, providerID)

	headers := map[string]string{
		"Content-Type":  "application/json",
		"Authorization": "Bearer " + apiKey,
		"User-Agent":    "CleverAIGate-HealthProbe/1.0",
	}

	// 1. EMBEDDING MODELS
	if capabilities != nil && capabilities["embedding"] || strings.Contains(modelLower, "embed") || strings.Contains(modelLower, "bge-") {
		reqURL := cleanBaseURL + "/v1/embeddings"
		if strings.HasSuffix(cleanBaseURL, "/v1") {
			reqURL = cleanBaseURL + "/embeddings"
		}
		bodyMap := map[string]interface{}{
			"model": targetModel,
			"input": "health check",
		}
		jsonBody, err := json.Marshal(bodyMap)
		if err != nil {
			return nil, fmt.Errorf("marshal embedding probe: %w", err)
		}
		return &ModelProbeRequest{Method: http.MethodPost, URL: reqURL, Headers: headers, Body: jsonBody}, nil
	}

	// 2. IMAGE GENERATION MODELS
	if capabilities != nil && capabilities["image_generation"] || strings.Contains(modelLower, "flux") || strings.Contains(modelLower, "dall-e") || strings.Contains(modelLower, "sdxl") {
		reqURL := cleanBaseURL + "/v1/images/generations"
		if strings.HasSuffix(cleanBaseURL, "/v1") {
			reqURL = cleanBaseURL + "/images/generations"
		}
		bodyMap := map[string]interface{}{
			"model":  targetModel,
			"prompt": "A simple white square",
			"n":      1,
			"size":   "256x256",
		}
		jsonBody, err := json.Marshal(bodyMap)
		if err != nil {
			return nil, fmt.Errorf("marshal image probe: %w", err)
		}
		return &ModelProbeRequest{Method: http.MethodPost, URL: reqURL, Headers: headers, Body: jsonBody}, nil
	}

	// 3. AUDIO / TTS MODELS
	if capabilities != nil && (capabilities["audio"] || capabilities["speech"] || capabilities["tts"]) || strings.Contains(modelLower, "tts") || strings.Contains(modelLower, "whisper") {
		reqURL := cleanBaseURL + "/v1/audio/speech"
		if strings.HasSuffix(cleanBaseURL, "/v1") {
			reqURL = cleanBaseURL + "/audio/speech"
		}
		bodyMap := map[string]interface{}{
			"model": targetModel,
			"input": "ping",
			"voice": "alloy",
		}
		jsonBody, err := json.Marshal(bodyMap)
		if err != nil {
			return nil, fmt.Errorf("marshal audio probe: %w", err)
		}
		return &ModelProbeRequest{Method: http.MethodPost, URL: reqURL, Headers: headers, Body: jsonBody}, nil
	}

	// 4. CHAT / REASONING / VISION / PARSE / GENERAL MODELS
	reqURL := cleanBaseURL + "/v1/chat/completions"
	if strings.HasSuffix(cleanBaseURL, "/v1") {
		reqURL = cleanBaseURL + "/chat/completions"
	}

	// Cloudflare Workers AI custom format
	if provider == "cloudflare" || strings.HasPrefix(cleanBaseURL, "cloudflare:") {
		accountID := strings.TrimPrefix(cleanBaseURL, "cloudflare:")
		reqURL = fmt.Sprintf("https://api.cloudflare.com/client/v4/accounts/%s/ai/v1/chat/completions", accountID)
	}

	var messages []map[string]interface{}

	// Special handling for parsing models requiring multimodal input structure
	if strings.Contains(modelLower, "parse") || strings.Contains(modelLower, "nemoretriever-parse") {
		messages = []map[string]interface{}{
			{
				"role": "user",
				"content": []map[string]interface{}{
					{"type": "text", "text": "ping"},
				},
			},
		}
	} else {
		messages = []map[string]interface{}{
			{"role": "user", "content": "ping"},
		}
	}

	bodyMap := map[string]interface{}{
		"model":      targetModel,
		"messages":   messages,
		"max_tokens": 5,
	}

	// Provider-specific parameter adjustments
	if provider == "nvidia" || strings.Contains(cleanBaseURL, "nvidia.com") {
		// Enforce positive temperature for NVIDIA NIM to avoid HTTP 422
		bodyMap["temperature"] = 0.7
	} else {
		bodyMap["temperature"] = 0.1
	}

	jsonBody, err := json.Marshal(bodyMap)
	if err != nil {
		return nil, fmt.Errorf("marshal chat probe: %w", err)
	}

	return &ModelProbeRequest{Method: http.MethodPost, URL: reqURL, Headers: headers, Body: jsonBody}, nil
}
