package credentials

// OneMinAIBaseURL is the root API URL for all 1min.ai endpoints.
const OneMinAIBaseURL = "https://api.1min.ai"

// OneMinAIModelEntry defines a single model in the 1min.ai catalog and how it
// maps to the gateway's routing infrastructure.
//
// 1min.ai does not expose a public /v1/models endpoint, so the full catalog is
// maintained as a static manifest. Each entry carries enough metadata for the
// discovery engine (pool creation + capability tagging) and the proxy hot-path
// (body translation + URL routing) to operate without additional network calls.
type OneMinAIModelEntry struct {
	// Model is the identifier sent to the 1min.ai API in the "model" field.
	// Example: "gpt-4o", "black-forest-labs/flux-schnell", "kling".
	Model string

	// Pattern is the gateway routing key stored in model_pools.model_pattern.
	// All 1min.ai patterns use the "1min/" prefix so the proxy handler can
	// detect them on the hot-path.
	// Example: "1min/gpt-4o", "1min/flux-schnell".
	Pattern string

	// Feature is the 1min.ai "type" field value.
	// Chat models use "UNIFY_CHAT_WITH_AI" (routed to /api/chat-with-ai).
	// All other modalities use their feature type (routed to /api/features).
	Feature string

	// Modality is one of: "chat", "code", "image", "audio_tts", "audio_stt", "video".
	// The proxy uses this to select the correct body translator and response translator.
	Modality string

	// SubModel is an optional modelName inside promptObject (used by some
	// video providers, e.g. Luma's "ray-v2"). Empty when not applicable.
	SubModel string
}

// oneminaiManifest is the complete catalog of 1min.ai models registered by the
// discovery engine. Organised by modality for readability.
//
// To add a new model: append an entry with the correct Model ID (as documented
// at https://docs.1min.ai), a unique Pattern, the Feature type, and Modality.
var oneminaiManifest = []OneMinAIModelEntry{
	// ── Chat / Writing (UNIFY_CHAT_WITH_AI → /api/chat-with-ai) ──────────────
	{Model: "gpt-4o", Pattern: "1min/gpt-4o", Feature: "UNIFY_CHAT_WITH_AI", Modality: "chat"},
	{Model: "gpt-4o-mini", Pattern: "1min/gpt-4o-mini", Feature: "UNIFY_CHAT_WITH_AI", Modality: "chat"},
	{Model: "gpt-4.1", Pattern: "1min/gpt-4.1", Feature: "UNIFY_CHAT_WITH_AI", Modality: "chat"},
	{Model: "gpt-4.1-mini", Pattern: "1min/gpt-4.1-mini", Feature: "UNIFY_CHAT_WITH_AI", Modality: "chat"},
	{Model: "claude-3-5-sonnet", Pattern: "1min/claude-3-5-sonnet", Feature: "UNIFY_CHAT_WITH_AI", Modality: "chat"},
	{Model: "claude-3-5-haiku", Pattern: "1min/claude-3-5-haiku", Feature: "UNIFY_CHAT_WITH_AI", Modality: "chat"},
	{Model: "claude-3-opus", Pattern: "1min/claude-3-opus", Feature: "UNIFY_CHAT_WITH_AI", Modality: "chat"},
	{Model: "gemini-2.0-flash", Pattern: "1min/gemini-2.0-flash", Feature: "UNIFY_CHAT_WITH_AI", Modality: "chat"},
	{Model: "gemini-2.5-pro", Pattern: "1min/gemini-2.5-pro", Feature: "UNIFY_CHAT_WITH_AI", Modality: "chat"},
	{Model: "deepseek-chat", Pattern: "1min/deepseek-chat", Feature: "UNIFY_CHAT_WITH_AI", Modality: "chat"},
	{Model: "deepseek-r1", Pattern: "1min/deepseek-r1", Feature: "UNIFY_CHAT_WITH_AI", Modality: "chat"},
	{Model: "llama-3.3-70b", Pattern: "1min/llama-3.3-70b", Feature: "UNIFY_CHAT_WITH_AI", Modality: "chat"},
	{Model: "mistral-large", Pattern: "1min/mistral-large", Feature: "UNIFY_CHAT_WITH_AI", Modality: "chat"},
	{Model: "qwen-2.5-72b", Pattern: "1min/qwen-2.5-72b", Feature: "UNIFY_CHAT_WITH_AI", Modality: "chat"},
	{Model: "grok-2", Pattern: "1min/grok-2", Feature: "UNIFY_CHAT_WITH_AI", Modality: "chat"},

	// ── Code (CODE_GENERATOR → /api/features) ────────────────────────────────
	{Model: "gpt-4o", Pattern: "1min/gpt-4o:code", Feature: "CODE_GENERATOR", Modality: "code"},
	{Model: "gpt-4o-mini", Pattern: "1min/gpt-4o-mini:code", Feature: "CODE_GENERATOR", Modality: "code"},
	{Model: "claude-3-5-sonnet", Pattern: "1min/claude-3-5-sonnet:code", Feature: "CODE_GENERATOR", Modality: "code"},
	{Model: "deepseek-chat", Pattern: "1min/deepseek-chat:code", Feature: "CODE_GENERATOR", Modality: "code"},

	// ── Image (IMAGE_GENERATOR → /api/features) ──────────────────────────────
	{Model: "black-forest-labs/flux-schnell", Pattern: "1min/flux-schnell", Feature: "IMAGE_GENERATOR", Modality: "image"},
	{Model: "black-forest-labs/flux-pro", Pattern: "1min/flux-pro", Feature: "IMAGE_GENERATOR", Modality: "image"},
	{Model: "black-forest-labs/flux-dev", Pattern: "1min/flux-dev", Feature: "IMAGE_GENERATOR", Modality: "image"},
	{Model: "black-forest-labs/flux-1.1-pro", Pattern: "1min/flux-1.1-pro", Feature: "IMAGE_GENERATOR", Modality: "image"},
	{Model: "dall-e-3", Pattern: "1min/dall-e-3", Feature: "IMAGE_GENERATOR", Modality: "image"},
	{Model: "stability-ai/sdxl", Pattern: "1min/sdxl", Feature: "IMAGE_GENERATOR", Modality: "image"},
	{Model: "stability-ai/sd-3.5", Pattern: "1min/sd-3.5", Feature: "IMAGE_GENERATOR", Modality: "image"},
	{Model: "ideogram", Pattern: "1min/ideogram", Feature: "IMAGE_GENERATOR", Modality: "image"},
	{Model: "recraft", Pattern: "1min/recraft", Feature: "IMAGE_GENERATOR", Modality: "image"},

	// ── Audio TTS (TEXT_TO_SPEECH → /api/features) ───────────────────────────
	{Model: "tts-1", Pattern: "1min/tts-1", Feature: "TEXT_TO_SPEECH", Modality: "audio_tts"},
	{Model: "tts-1-hd", Pattern: "1min/tts-1-hd", Feature: "TEXT_TO_SPEECH", Modality: "audio_tts"},

	// ── Audio STT (SPEECH_TO_TEXT → /api/features) ───────────────────────────
	{Model: "whisper-1", Pattern: "1min/whisper-1", Feature: "SPEECH_TO_TEXT", Modality: "audio_stt"},

	// ── Video (TEXT_TO_VIDEO → /api/features) ────────────────────────────────
	{Model: "luma", Pattern: "1min/luma", Feature: "TEXT_TO_VIDEO", Modality: "video", SubModel: "ray-v2"},
	{Model: "kling", Pattern: "1min/kling", Feature: "TEXT_TO_VIDEO", Modality: "video"},
	{Model: "hunyuan", Pattern: "1min/hunyuan", Feature: "TEXT_TO_VIDEO", Modality: "video"},
	{Model: "veo3", Pattern: "1min/veo3", Feature: "TEXT_TO_VIDEO", Modality: "video"},
	{Model: "sora", Pattern: "1min/sora", Feature: "TEXT_TO_VIDEO", Modality: "video"},
	{Model: "hailuo", Pattern: "1min/hailuo", Feature: "TEXT_TO_VIDEO", Modality: "video"},
	{Model: "pika", Pattern: "1min/pika", Feature: "TEXT_TO_VIDEO", Modality: "video"},
}

// oneminaiModelMap is built once at package init for O(1) lookups.
// Each entry is indexed by BOTH its gateway routing pattern (e.g. "1min/dall-e-3")
// AND its clean upstream model name (e.g. "dall-e-3"). This allows the proxy
// body/response translators to resolve feature metadata regardless of whether
// the client sends the prefixed form ("1min/dall-e-3") or the clean standard
// name ("dall-e-3") — which is required when clean-name alias pools are registered
// during discovery so that client tools with hardcoded model name whitelists work.
var oneminaiModelMap = func() map[string]OneMinAIModelEntry {
	// Allocate double capacity: one slot per pattern + one slot per clean model name.
	m := make(map[string]OneMinAIModelEntry, len(oneminaiManifest)*2)
	for _, e := range oneminaiManifest {
		// Primary key: full gateway routing pattern (e.g. "1min/dall-e-3")
		m[e.Pattern] = e
		// Alias key: clean upstream model name (e.g. "dall-e-3").
		// Only add if it differs from the pattern to avoid a redundant write.
		if e.Model != e.Pattern {
			m[e.Model] = e
		}
	}
	return m
}()

// LookupOneMinAIModel returns the manifest entry for a given key. The key may
// be either the gateway routing pattern (e.g. "1min/gpt-4o") or the clean
// upstream model name (e.g. "dall-e-3", "whisper-1"). The second return value
// is false if the key does not match any registered 1min.ai model.
//
// Used by:
//   - The proxy path transformer (rewriter.go) to decide /api/chat-with-ai vs /api/features
//   - The proxy body translator (oneminai.go) to build the correct 1min.ai request
//   - The proxy response translator (oneminai.go) to parse the 1min.ai response
func LookupOneMinAIModel(pattern string) (OneMinAIModelEntry, bool) {
	entry, ok := oneminaiModelMap[pattern]
	return entry, ok
}

// OneMinAIManifest returns a copy of the full model manifest.
// Used by the discovery engine to iterate all models for registration.
func OneMinAIManifest() []OneMinAIModelEntry {
	result := make([]OneMinAIModelEntry, len(oneminaiManifest))
	copy(result, oneminaiManifest)
	return result
}
