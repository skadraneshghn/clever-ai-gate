package proxy

import (
	"regexp"
	"strings"

	"github.com/skadraneshghn/clever-ai-gate/internal/credentials"
)

// KnownProviderPrefixes lists standard routing namespaces used in the gateway.
var KnownProviderPrefixes = []string{
	"nvidia/",
	"openrouter/",
	"ollama/",
	"gemini/",
	"1min/",
	"cloudflare/",
	"sarvam/",
	"puter/",
	"agentrouter/",
	"zenmux/",
	"jiekou/",
	"novita/",
	"together/",
	"groq/",
	"fireworks/",
	"mistral/",
	"perplexity/",
}

// KnownPublisherPrefixes lists common model vendor/organization namespaces.
var KnownPublisherPrefixes = []string{
	"@cf/meta/",
	"@cf/mistral/",
	"@cf/qwen/",
	"@cf/google/",
	"@cf/deepseek-ai/",
	"@cf/baai/",
	"@cf/defog/",
	"meta-llama/",
	"meta/",
	"deepseek-ai/",
	"deepseek/",
	"openai/",
	"anthropic/",
	"google/",
	"mistralai/",
	"mistral/",
	"qwen/",
	"01-ai/",
	"baichuan-inc/",
	"z-ai/",
	"thudm/",
	"bigcode/",
	"databricks/",
	"microsoft/",
	"cohere/",
}

var versionDotRegex = regexp.MustCompile(`(\d+)\.(\d+)`)

// StripProviderPrefix removes any known or custom leading provider routing namespace.
func StripProviderPrefix(pattern string) (cleanModel, provider string) {
	for _, p := range KnownProviderPrefixes {
		if strings.HasPrefix(pattern, p) {
			return pattern[len(p):], strings.TrimSuffix(p, "/")
		}
	}
	// Also handle any arbitrary custom single-segment prefix e.g. "myprovider/foo"
	if idx := strings.IndexByte(pattern, '/'); idx != -1 {
		prefix := pattern[:idx]
		isPublisher := false
		for _, pub := range KnownPublisherPrefixes {
			if strings.TrimSuffix(pub, "/") == prefix {
				isPublisher = true
				break
			}
		}
		if !isPublisher {
			return pattern[idx+1:], prefix
		}
	}
	return pattern, ""
}

// FormatUpstreamModelForPattern returns the clean model ID expected in the upstream
// request body when routing to the given pool pattern.
func FormatUpstreamModelForPattern(pattern string) string {
	clean, _ := StripProviderPrefix(pattern)
	return clean
}

// ExtractCoreModelAndVersion extracts the canonical model ID and version from a model
// pattern by stripping routing prefixes, vendor namespaces, and normalizing version separators.
//
// Examples:
//
//	"nvidia/meta/llama-3.3-70b-instruct"          -> "llama-3-3-70b-instruct"
//	"openrouter/meta-llama/llama-3.3-70b-instruct" -> "llama-3-3-70b-instruct"
//	"cloudflare/@cf/meta/llama-3.3-70b-instruct"   -> "llama-3-3-70b-instruct"
//	"ollama/llama3.3:70b-instruct"                 -> "llama-3-3-70b-instruct"
//	"puter/gpt-4o"                                 -> "gpt-4o"
//	"openrouter/openai/gpt-4o"                     -> "gpt-4o"
//	"deepseek/deepseek-r1:free"                    -> "deepseek-r1"
//	"nvidia/deepseek-ai/deepseek-r1"               -> "deepseek-r1"
//	"anthropic/claude-3-5-sonnet-20241022"         -> "claude-3-5-sonnet-20241022"
//	"claude-3.5-sonnet"                            -> "claude-3-5-sonnet"
func ExtractCoreModelAndVersion(pattern string) string {
	raw := strings.ToLower(strings.TrimSpace(pattern))
	if raw == "" {
		return ""
	}

	// 1. Strip common suffixes like :free, :latest, :beta, :preview
	raw = strings.TrimSuffix(raw, ":free")
	raw = strings.TrimSuffix(raw, ":latest")
	raw = strings.TrimSuffix(raw, ":beta")
	raw = strings.TrimSuffix(raw, ":preview")

	// 2. Strip leading routing provider prefix (e.g. nvidia/, openrouter/, puter/)
	for _, p := range KnownProviderPrefixes {
		if strings.HasPrefix(raw, p) {
			raw = strings.TrimPrefix(raw, p)
			break
		}
	}

	// 3. Strip publisher / organization prefixes (e.g. meta-llama/, deepseek-ai/, @cf/meta/)
	for _, pub := range KnownPublisherPrefixes {
		if strings.HasPrefix(raw, pub) {
			raw = strings.TrimPrefix(raw, pub)
			break
		}
	}

	// 4. If any slash remains (e.g. custom provider "myprov/meta/llama-3.3-70b"),
	// take the final segment if it appears to be the model name
	if idx := strings.LastIndexByte(raw, '/'); idx != -1 {
		raw = raw[idx+1:]
	}

	// 5. Normalize version dots to hyphens: e.g. "3.5" -> "3-5", "3.3" -> "3-3"
	// This ensures "claude-3.5-sonnet" and "claude-3-5-sonnet" match identically.
	raw = versionDotRegex.ReplaceAllString(raw, "${1}-${2}")

	// 6. Normalize colon separators (common in Ollama tags e.g. "llama3-3:70b" -> "llama3-3-70b")
	raw = strings.ReplaceAll(raw, ":", "-")

	// 7. Normalize "llama3-3" with "llama-3-3"
	if strings.HasPrefix(raw, "llama3-") {
		raw = "llama-3-" + strings.TrimPrefix(raw, "llama3-")
	} else if strings.HasPrefix(raw, "llama2-") {
		raw = "llama-2-" + strings.TrimPrefix(raw, "llama2-")
	}

	return strings.TrimSpace(raw)
}

// IsSameModelAndVersion returns true if patternA and patternB refer to the exact same
// model architecture, parameter size, and version.
func IsSameModelAndVersion(patternA, patternB string) bool {
	coreA := ExtractCoreModelAndVersion(patternA)
	coreB := ExtractCoreModelAndVersion(patternB)
	return coreA != "" && coreA == coreB
}

// FindExactModelFallbacks searches all loaded routing pools for other pools that host
// the exact same model and version as currentModel, excluding already tried pools and
// pools with zero healthy credentials.
func FindExactModelFallbacks(currentModel string, triedPools map[string]bool, allPools map[string]*credentials.BalancedChannelPool) []*credentials.BalancedChannelPool {
	targetCore := ExtractCoreModelAndVersion(currentModel)
	if targetCore == "" || len(allPools) == 0 {
		return nil
	}

	var candidates []*credentials.BalancedChannelPool
	seen := make(map[string]bool)

	for candPattern, candPool := range allPools {
		if candPool == nil {
			continue
		}
		// Never retry the same pattern or an already tried pool
		if candPattern == currentModel || candPool.ModelPattern == currentModel ||
			triedPools[candPattern] || triedPools[candPool.ModelPattern] ||
			seen[candPool.ModelPattern] {
			continue
		}

		// Must be the EXACT same model ID and version
		candCore := ExtractCoreModelAndVersion(candPattern)
		if candCore != targetCore {
			continue
		}

		// Must have at least one healthy credential past cooldown
		if candPool.HealthyCount() == 0 {
			continue
		}

		seen[candPool.ModelPattern] = true
		candidates = append(candidates, candPool)
	}

	return candidates
}
