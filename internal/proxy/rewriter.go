package proxy

import (
	"fmt"
	"net/http"
	"strings"
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

	// NVIDIA NIM
	r.pathTransformers["nvidia"] = passthroughPath
	r.pathTransformers["xai"] = passthroughPath

	// Ollama (OpenAI-compatible at /v1/chat/completions)
	r.pathTransformers["ollama"] = passthroughPath

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
	// Always set JSON content type
	req.Header.Set("Content-Type", "application/json")

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
