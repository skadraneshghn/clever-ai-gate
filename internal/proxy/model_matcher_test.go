package proxy

import (
	"testing"
	"time"

	"github.com/skadraneshghn/clever-ai-gate/internal/credentials"
)

func TestExtractCoreModelAndVersion(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		// NVIDIA NIM
		{"nvidia/meta/llama-3.3-70b-instruct", "llama-3-3-70b-instruct"},
		{"nvidia/deepseek-ai/deepseek-r1", "deepseek-r1"},
		{"nvidia/z-ai/glm-5.1", "glm-5-1"},

		// OpenRouter
		{"openrouter/meta-llama/llama-3.3-70b-instruct", "llama-3-3-70b-instruct"},
		{"openrouter/openai/gpt-4o", "gpt-4o"},
		{"openrouter/deepseek/deepseek-r1:free", "deepseek-r1"},
		{"openrouter/anthropic/claude-3.5-sonnet:beta", "claude-3-5-sonnet"},

		// Cloudflare Workers AI
		{"cloudflare/@cf/meta/llama-3.3-70b-instruct", "llama-3-3-70b-instruct"},
		{"cloudflare/@cf/deepseek-ai/deepseek-r1", "deepseek-r1"},

		// Ollama
		{"ollama/llama3.3:70b-instruct", "llama-3-3-70b-instruct"},
		{"ollama/llama3:8b", "llama-3-8b"},

		// Puter
		{"puter/gpt-4o", "gpt-4o"},
		{"puter/gpt-4o-mini", "gpt-4o-mini"},

		// Clean names / standalone
		{"gpt-4o", "gpt-4o"},
		{"claude-3.5-sonnet", "claude-3-5-sonnet"},
		{"claude-3-5-sonnet", "claude-3-5-sonnet"},
		{"claude-3-5-sonnet-20241022", "claude-3-5-sonnet-20241022"},
	}

	for _, tc := range tests {
		actual := ExtractCoreModelAndVersion(tc.input)
		if actual != tc.expected {
			t.Errorf("ExtractCoreModelAndVersion(%q) = %q; want %q", tc.input, actual, tc.expected)
		}
	}
}

func TestIsSameModelAndVersion(t *testing.T) {
	// Should MATCH (exact same architecture, size, and version across providers)
	matches := [][2]string{
		{"nvidia/meta/llama-3.3-70b-instruct", "openrouter/meta-llama/llama-3.3-70b-instruct"},
		{"nvidia/meta/llama-3.3-70b-instruct", "cloudflare/@cf/meta/llama-3.3-70b-instruct"},
		{"openrouter/meta-llama/llama-3.3-70b-instruct", "cloudflare/@cf/meta/llama-3.3-70b-instruct"},
		{"puter/gpt-4o", "openrouter/openai/gpt-4o"},
		{"openrouter/deepseek/deepseek-r1:free", "nvidia/deepseek-ai/deepseek-r1"},
		{"anthropic/claude-3.5-sonnet", "openrouter/anthropic/claude-3-5-sonnet"},
	}

	for _, pair := range matches {
		if !IsSameModelAndVersion(pair[0], pair[1]) {
			t.Errorf("Expected IsSameModelAndVersion(%q, %q) = true, got false", pair[0], pair[1])
		}
	}

	// Should NOT match (different version, size, or completely different model)
	mismatches := [][2]string{
		// Different versions: 3.1 vs 3.3
		{"nvidia/meta/llama-3.1-70b-instruct", "nvidia/meta/llama-3.3-70b-instruct"},
		{"openrouter/meta-llama/llama-3.1-70b-instruct", "cloudflare/@cf/meta/llama-3.3-70b-instruct"},

		// Different sizes: 8b vs 70b
		{"nvidia/meta/llama-3.3-8b-instruct", "nvidia/meta/llama-3.3-70b-instruct"},
		{"cloudflare/@cf/meta/llama-3.2-3b-instruct", "cloudflare/@cf/meta/llama-3.2-1b-instruct"},

		// Arbitrary / downgraded models: gpt-4o vs gpt-4o-mini
		{"puter/gpt-4o", "puter/gpt-4o-mini"},
		{"puter/gpt-4o", "cloudflare/@cf/meta/llama-3.2-3b-instruct"},
		{"claude-3-5-haiku", "claude-3-5-sonnet"},
	}

	for _, pair := range mismatches {
		if IsSameModelAndVersion(pair[0], pair[1]) {
			t.Errorf("Expected IsSameModelAndVersion(%q, %q) = false, got true", pair[0], pair[1])
		}
	}
}

func TestFindExactModelFallbacks(t *testing.T) {
	// Setup mock pools
	nvidiaPool := credentials.NewBalancedPool("nvidia/meta/llama-3.3-70b-instruct", "round-robin", []*credentials.RuntimeCredential{
		{ID: 1, APIKey: "nv-1", Provider: "nvidia", BaseURL: "https://integrate.api.nvidia.com"},
	}, nil)
	openrouterPool := credentials.NewBalancedPool("openrouter/meta-llama/llama-3.3-70b-instruct", "round-robin", []*credentials.RuntimeCredential{
		{ID: 2, APIKey: "or-1", Provider: "openrouter", BaseURL: "https://openrouter.ai/api"},
	}, nil)
	cloudflarePool := credentials.NewBalancedPool("cloudflare/@cf/meta/llama-3.3-70b-instruct", "round-robin", []*credentials.RuntimeCredential{
		{ID: 3, APIKey: "cf-1", Provider: "cloudflare", BaseURL: "https://api.cloudflare.com"},
	}, nil)
	differentVersionPool := credentials.NewBalancedPool("openrouter/meta-llama/llama-3.1-70b-instruct", "round-robin", []*credentials.RuntimeCredential{
		{ID: 4, APIKey: "or-2", Provider: "openrouter", BaseURL: "https://openrouter.ai/api"},
	}, nil)
	cheaperModelPool := credentials.NewBalancedPool("puter/gpt-4o-mini", "round-robin", []*credentials.RuntimeCredential{
		{ID: 5, APIKey: "put-1", Provider: "puter", BaseURL: "https://api.puter.com"},
	}, nil)

	allPools := map[string]*credentials.BalancedChannelPool{
		"nvidia/meta/llama-3.3-70b-instruct":          nvidiaPool,
		"openrouter/meta-llama/llama-3.3-70b-instruct": openrouterPool,
		"cloudflare/@cf/meta/llama-3.3-70b-instruct":   cloudflarePool,
		"openrouter/meta-llama/llama-3.1-70b-instruct": differentVersionPool,
		"puter/gpt-4o-mini":                           cheaperModelPool,
	}

	triedPools := map[string]bool{
		"nvidia/meta/llama-3.3-70b-instruct": true,
	}

	fallbacks := FindExactModelFallbacks("nvidia/meta/llama-3.3-70b-instruct", triedPools, allPools)

	if len(fallbacks) != 2 {
		t.Fatalf("expected 2 fallbacks, got %d", len(fallbacks))
	}

	foundOR := false
	foundCF := false
	for _, fb := range fallbacks {
		if fb.ModelPattern == "openrouter/meta-llama/llama-3.3-70b-instruct" {
			foundOR = true
		}
		if fb.ModelPattern == "cloudflare/@cf/meta/llama-3.3-70b-instruct" {
			foundCF = true
		}
		if fb.ModelPattern == "openrouter/meta-llama/llama-3.1-70b-instruct" {
			t.Errorf("FindExactModelFallbacks included different version llama-3.1!")
		}
		if fb.ModelPattern == "puter/gpt-4o-mini" {
			t.Errorf("FindExactModelFallbacks included arbitrary model gpt-4o-mini!")
		}
	}

	if !foundOR || !foundCF {
		t.Errorf("expected openrouter and cloudflare fallbacks, got OR=%v, CF=%v", foundOR, foundCF)
	}

	// Now penalize openrouterPool so it has 0 healthy keys
	openrouterPool.PenalizeToken(0, 10*time.Minute)
	fallbacksAfterPenalize := FindExactModelFallbacks("nvidia/meta/llama-3.3-70b-instruct", triedPools, allPools)
	if len(fallbacksAfterPenalize) != 1 || fallbacksAfterPenalize[0].ModelPattern != "cloudflare/@cf/meta/llama-3.3-70b-instruct" {
		t.Errorf("expected only healthy cloudflare fallback after openrouter penalized, got %v", fallbacksAfterPenalize)
	}
}

func TestFormatUpstreamModelForPattern(t *testing.T) {
	tests := []struct {
		pattern  string
		expected string
	}{
		{"nvidia/meta/llama-3.3-70b-instruct", "meta/llama-3.3-70b-instruct"},
		{"openrouter/meta-llama/llama-3.3-70b-instruct", "meta-llama/llama-3.3-70b-instruct"},
		{"cloudflare/@cf/meta/llama-3.3-70b-instruct", "@cf/meta/llama-3.3-70b-instruct"},
		{"puter/gpt-4o", "gpt-4o"},
		{"ollama/llama3.3:70b", "llama3.3:70b"},
		{"gpt-4o", "gpt-4o"},
	}

	for _, tc := range tests {
		actual := FormatUpstreamModelForPattern(tc.pattern)
		if actual != tc.expected {
			t.Errorf("FormatUpstreamModelForPattern(%q) = %q; want %q", tc.pattern, actual, tc.expected)
		}
	}
}
