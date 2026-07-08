package credentials

// SarvamBaseURL is the root API URL for Sarvam AI.
// Sarvam is natively OpenAI-compatible: chat completions are served at
// {SarvamBaseURL}/v1/chat/completions with the standard OpenAI request/response
// schema and SSE streaming (chat.completion.chunk deltas, reasoning_content).
const SarvamBaseURL = "https://api.sarvam.ai"

// SarvamModelEntry defines a single Sarvam AI chat model and how it maps to the
// gateway's routing infrastructure.
//
// Sarvam AI does not expose a public /v1/models endpoint; the chat-completion
// model enum is fixed (sarvam-30b, sarvam-105b). The full catalog is therefore
// maintained as a static manifest — the same approach used for 1min.ai.
type SarvamModelEntry struct {
	// Model is the upstream identifier sent to Sarvam in the "model" field.
	// Example: "sarvam-105b".
	Model string

	// Pattern is the gateway routing key stored in model_pools.model_pattern.
	// All Sarvam patterns use the "sarvam/" prefix so the proxy handler can
	// detect them on the hot-path.
	// Example: "sarvam/sarvam-105b".
	Pattern string

	// ContextWindow is the model's maximum input context in tokens (metadata).
	ContextWindow int
}

// sarvamManifest is the complete catalog of Sarvam AI OpenAI-compatible chat
// models registered by the discovery engine.
//
// Sarvam's other models (Saaras speech-to-text, Bulbul text-to-speech,
// Mayura/Sarvam-Translate, Sarvam Vision) use non-OpenAI REST schemas and are
// intentionally excluded — they do not fit the gateway's /v1/chat/completions
// front. "All models" therefore means all OpenAI-compatible chat models.
var sarvamManifest = []SarvamModelEntry{
	{Model: "sarvam-30b", Pattern: "sarvam/sarvam-30b", ContextWindow: 65536},
	{Model: "sarvam-105b", Pattern: "sarvam/sarvam-105b", ContextWindow: 131072},
}

// SarvamManifest returns a copy of the full Sarvam model manifest.
// Used by the discovery engine to iterate all models for registration.
func SarvamManifest() []SarvamModelEntry {
	result := make([]SarvamModelEntry, len(sarvamManifest))
	copy(result, sarvamManifest)
	return result
}
