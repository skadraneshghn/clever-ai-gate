package proxy

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/skadraneshghn/clever-ai-gate/internal/credentials"
)

// Rewriter handles URL path and header translation for different AI providers.
// Each provider has its own API structure, authentication format, and URL scheme.
type Rewriter struct {
	// Provider-specific path transformers
	pathTransformers map[string]PathTransformer
}

// PathTransformer defines how to transform a URL path for a specific provider.
type PathTransformer func(basePath, requestPath, model string) string

// NewRewriter creates a new URL/header rewriter with all provider transformers registered.
func NewRewriter() *Rewriter {
	r := &Rewriter{
		pathTransformers: make(map[string]PathTransformer),
	}

	// OpenAI-compatible providers (passthrough)
	r.pathTransformers["openai"] = passthroughPath
	r.pathTransformers["deepseek"] = passthroughPath
	r.pathTransformers["groq"] = passthroughPath
	r.pathTransformers["together"] = passthroughPath
	r.pathTransformers["openrouter"] = passthroughPath
	r.pathTransformers["fireworks"] = passthroughPath
	r.pathTransformers["mistral"] = passthroughPath
	r.pathTransformers["perplexity"] = passthroughPath

	// Generic OpenAI-compatible (any third-party provider)
	r.pathTransformers["custom"] = passthroughPath

	// NVIDIA NIM
	r.pathTransformers["nvidia"] = passthroughPath
	r.pathTransformers["xai"] = passthroughPath

	// Ollama: uses native /api/* paths for Ollama Cloud (https://ollama.com)
	// and OpenAI-compatible /v1/* passthrough for local instances.
	r.pathTransformers["ollama"] = ollamaPath

	// Anthropic
	r.pathTransformers["anthropic"] = anthropicPath

	// Google Gemini
	r.pathTransformers["gemini"] = geminiPath

	// Google Vertex AI
	r.pathTransformers["vertex"] = vertexPath

	// Azure OpenAI
	r.pathTransformers["azure"] = azurePath

	// AWS Bedrock
	r.pathTransformers["bedrock"] = bedrockPath

	// Cohere
	r.pathTransformers["cohere"] = coherePath

	// 1min.ai: multi-modal Feature API (chat, code, image, audio, video)
	r.pathTransformers["1minai"] = oneminaiPath

	// Cloudflare Workers AI: OpenAI-compatible completions + universal run endpoint
	r.pathTransformers["cloudflare"] = cloudflarePath

	// Sarvam AI: natively OpenAI-compatible (POST /v1/chat/completions, SSE streaming)
	r.pathTransformers["sarvam"] = passthroughPath

	return r
}

// RewriteURL transforms the incoming URL to the target provider's format.
func (r *Rewriter) RewriteURL(provider, baseURL, requestPath, model string) string {
	// Remove trailing slash from base URL
	baseURL = strings.TrimRight(baseURL, "/")

	// Prevent duplicate /v1 in path (e.g. base_url/v1 + /v1/chat/completions)
	if strings.HasSuffix(baseURL, "/v1") && strings.HasPrefix(requestPath, "/v1/") {
		baseURL = strings.TrimSuffix(baseURL, "/v1")
	}

	transformer, ok := r.pathTransformers[provider]
	if !ok {
		// Unknown provider — assume OpenAI-compatible
		return baseURL + requestPath
	}

	return transformer(baseURL, requestPath, model)
}

// RewriteHeaders sets the appropriate authentication and content headers
// for the target provider.
func (r *Rewriter) RewriteHeaders(req *http.Request, provider, apiKey string, sourceHeaders http.Header) {
	// Content-Type handling: multipart/form-data payloads (audio transcriptions,
	// image uploads, file uploads) include a boundary string that must be
	// preserved exactly. Forcing application/json would destroy the payload.
	if ct := sourceHeaders.Get("Content-Type"); strings.HasPrefix(ct, "multipart/") {
		req.Header.Set("Content-Type", ct)
	} else if ct != "" {
		req.Header.Set("Content-Type", ct)
	} else {
		req.Header.Set("Content-Type", "application/json")
	}

	// Copy relevant headers from original request
	if accept := sourceHeaders.Get("Accept"); accept != "" {
		req.Header.Set("Accept", accept)
	}

	// Provider-specific authentication
	switch provider {
	case "anthropic":
		req.Header.Set("x-api-key", apiKey)
		req.Header.Set("anthropic-version", "2023-06-01")
		// Enable extended thinking if present in original request
		if sourceHeaders.Get("anthropic-beta") != "" {
			req.Header.Set("anthropic-beta", sourceHeaders.Get("anthropic-beta"))
		}

	case "gemini":
		// Gemini uses API key as query parameter (handled in URL rewrite)
		// But also supports Bearer token for OAuth
		req.Header.Set("Authorization", "Bearer "+apiKey)

	case "azure":
		req.Header.Set("api-key", apiKey)

	case "cohere":
		req.Header.Set("Authorization", "Bearer "+apiKey)

	case "bedrock":
		// AWS Bedrock uses SigV4 signing — the API key here is a pre-signed token
		req.Header.Set("Authorization", "Bearer "+apiKey)

	case "1minai":
		// 1min.ai uses a custom API-KEY header (not Bearer)
		req.Header.Set("API-KEY", apiKey)

	case "sarvam":
		// Sarvam prefers the api-subscription-key header but also accepts
		// Authorization: Bearer on all endpoints. Send both for bulletproof
		// auth regardless of backend routing changes at Sarvam.
		req.Header.Set("api-subscription-key", apiKey)
		req.Header.Set("Authorization", "Bearer "+apiKey)

	default:
		// OpenAI and OpenAI-compatible providers
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	// Forward user agent for debugging
	if ua := sourceHeaders.Get("User-Agent"); ua != "" {
		req.Header.Set("User-Agent", "CleverAIGate/1.0 ("+ua+")")
	} else {
		req.Header.Set("User-Agent", "CleverAIGate/1.0")
	}
}

// --- Path transformers ---

// passthroughPath forwards the path as-is (OpenAI-compatible providers).
func passthroughPath(baseURL, requestPath, _ string) string {
	return baseURL + requestPath
}

// anthropicPath transforms OpenAI paths to Anthropic's API format.
// /v1/chat/completions → /v1/messages
func anthropicPath(baseURL, requestPath, _ string) string {
	if strings.Contains(requestPath, "/chat/completions") {
		return baseURL + "/v1/messages"
	}
	return baseURL + requestPath
}

// geminiPath transforms to Gemini's REST API format.
// /v1/chat/completions → /v1beta/models/{model}:generateContent
// /v1/chat/completions (stream) → /v1beta/models/{model}:streamGenerateContent
func geminiPath(baseURL, requestPath, model string) string {
	if strings.Contains(requestPath, "/chat/completions") {
		// Default to streaming — the handler will switch based on stream flag
		return fmt.Sprintf("%s/v1beta/models/%s:streamGenerateContent?alt=sse", baseURL, model)
	}
	if strings.Contains(requestPath, "/embeddings") {
		return fmt.Sprintf("%s/v1beta/models/%s:embedContent", baseURL, model)
	}
	return baseURL + requestPath
}

// vertexPath transforms to Vertex AI's API format.
func vertexPath(baseURL, requestPath, model string) string {
	if strings.Contains(requestPath, "/chat/completions") {
		return fmt.Sprintf("%s/v1/projects/-/locations/-/publishers/google/models/%s:streamGenerateContent", baseURL, model)
	}
	return baseURL + requestPath
}

// azurePath transforms to Azure OpenAI's deployment-based format.
func azurePath(baseURL, requestPath, model string) string {
	if strings.Contains(requestPath, "/chat/completions") {
		return fmt.Sprintf("%s/openai/deployments/%s/chat/completions?api-version=2024-02-15-preview", baseURL, model)
	}
	if strings.Contains(requestPath, "/embeddings") {
		return fmt.Sprintf("%s/openai/deployments/%s/embeddings?api-version=2024-02-15-preview", baseURL, model)
	}
	return baseURL + requestPath
}

// bedrockPath transforms to AWS Bedrock's invoke format.
func bedrockPath(baseURL, requestPath, model string) string {
	if strings.Contains(requestPath, "/chat/completions") {
		return fmt.Sprintf("%s/model/%s/invoke", baseURL, model)
	}
	return baseURL + requestPath
}

// coherePath transforms to Cohere's API format.
func coherePath(baseURL, requestPath, _ string) string {
	if strings.Contains(requestPath, "/chat/completions") {
		return baseURL + "/v2/chat"
	}
	return baseURL + requestPath
}

// Note: NVIDIA NIM uses passthroughPath since it is OpenAI-compatible.
// Base URL should be configured as https://integrate.api.nvidia.com/v1
// and the path /v1/chat/completions passes through directly.

// oneminaiPath transforms OpenAI-compatible paths to 1min.ai's Feature API format.
//
//	/v1/chat/completions  → /api/chat-with-ai  (chat models)
//	                     → /api/features       (code & video models)
//	/v1/images/generations → /api/features      (image models)
//	/v1/audio/speech       → /api/features      (TTS models)
//	/v1/audio/transcriptions → /api/features    (STT models)
//
// The modality is determined by looking up the model in the 1min.ai manifest.
// Code and video models are routed to /api/features even when the incoming
// path is /v1/chat/completions, because they use the Feature API.
func oneminaiPath(baseURL, requestPath, model string) string {
	// Image and audio endpoints always go to the Feature API
	if strings.Contains(requestPath, "/images/") ||
		strings.Contains(requestPath, "/audio/") {
		return baseURL + "/api/features"
	}

	// Chat completions: route to chat-with-ai or features depending on modality
	if strings.Contains(requestPath, "/chat/completions") ||
		strings.Contains(requestPath, "/completions") {
		if entry, ok := credentials.LookupOneMinAIModel(model); ok {
			if entry.Modality != "chat" {
				return baseURL + "/api/features"
			}
		}
		return baseURL + "/api/chat-with-ai"
	}

	// Default: Feature API
	return baseURL + "/api/features"
}

// ollamaPath routes requests to the correct Ollama endpoint based on the
// base URL. Ollama Cloud (https://ollama.com) exposes a native REST API:
//
//	/v1/chat/completions  → /api/chat
//	/v1/completions       → /api/generate
//	/v1/embeddings        → /api/embeddings
//
// Local Ollama instances (http://localhost:11434 or custom self-hosted) expose
// an OpenAI-compatible API at /v1/*, so we fall through to passthroughPath.
func ollamaPath(baseURL, requestPath, _ string) string {
	// Route to native Ollama Cloud API endpoints only when the base URL
	// points to the official Ollama Cloud host.
	if strings.Contains(baseURL, "ollama.com") {
		if strings.Contains(requestPath, "/chat/completions") {
			return baseURL + "/api/chat"
		}
		if strings.Contains(requestPath, "/completions") {
			return baseURL + "/api/generate"
		}
		if strings.Contains(requestPath, "/embeddings") {
			return baseURL + "/api/embeddings"
		}
		// Default: use the native API path as-is
		return baseURL + requestPath
	}
	// Local / self-hosted Ollama: passthrough OpenAI-compatible paths unchanged.
	return baseURL + requestPath
}

// cloudflarePath routes requests to the correct Cloudflare Workers AI endpoint.
//
// The base_url convention used by this gateway is "cloudflare:<accountID>".
// The account ID is extracted via strings.TrimPrefix.
//
// Two downstream paths exist:
//
//	Text completions (/v1/chat/completions, /v1/completions):
//	  → https://api.cloudflare.com/client/v4/accounts/{accountID}/ai/v1/chat/completions
//	  (Cloudflare's native OpenAI-compatible endpoint, supports streaming SSE)
//
//	All other paths (/v1/embeddings, /v1/images/generations, /v1/audio/*):
//	  → https://api.cloudflare.com/client/v4/accounts/{accountID}/ai/run/{model}
//	  (Cloudflare's universal inference endpoint)
func cloudflarePath(baseURL, requestPath, model string) string {
	// Parse the account ID from the stored base_url convention.
	accountID := strings.TrimPrefix(baseURL, "cloudflare:")
	cfBase := "https://api.cloudflare.com/client/v4/accounts/" + accountID

	if strings.Contains(requestPath, "/chat/completions") ||
		strings.Contains(requestPath, "/completions") {
		// OpenAI-compatible endpoint — supports streaming and all standard parameters.
		return cfBase + "/ai/v1/chat/completions"
	}

	// All other modalities (embeddings, images, audio, etc.) use the universal
	// /ai/run/{model} endpoint. The model value here is already the clean upstream
	// ID (e.g. "@cf/baai/bge-base-en-v1.5") with the "cloudflare/" prefix stripped
	// by the handler before calling RewriteURL.
	return cfBase + "/ai/run/" + model
}
