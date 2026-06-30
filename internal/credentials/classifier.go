package credentials

import "strings"

// ModelCapabilities holds the detected capability flags for a model.
// Each bool indicates whether the model is expected to support that feature,
// inferred from the model identifier using substring pattern matching.
//
// These are heuristic guesses — not guaranteed, but highly accurate for
// well-known model families (GPT, Claude, Llama, Whisper, DALL-E, etc.).
type ModelCapabilities struct {
	Reasoning       bool `json:"reasoning,omitempty"`
	Vision          bool `json:"vision,omitempty"`
	ImageGeneration bool `json:"image_generation,omitempty"`
	Audio           bool `json:"audio,omitempty"`
	Code            bool `json:"code,omitempty"`
	Embedding       bool `json:"embedding,omitempty"`
}

// ClassifyModel examines a model ID and returns its inferred capabilities.
// The algorithm performs a single lowercase pass and checks substring membership.
// It allocates no heap objects other than the returned struct.
//
// Rule sets (applied to lowercase model ID):
//
//	reasoning:        o1-, o3-, o4-, -r1, thinking, reasoning, nemotron, qwq, deepthink
//	vision:           vision, llava, pixtral, gpt-4o, claude-3, gemini, qvq, -vl, vl-, bakllava
//	image_generation: dall-e, stable-diffusion, flux, midjourney, imagen, sdxl, wanx
//	audio:            whisper, tts, audio, speech, voix
//	code:             code, coder, codex, starcoder, deepseek-coder, qwen-coder, magicoder
//	embedding:        embed, embedding, text-embedding, e5-, bge-
func ClassifyModel(modelID string) ModelCapabilities {
	lower := strings.ToLower(modelID)
	return ModelCapabilities{
		Reasoning:       hasReasoning(lower),
		Vision:          hasVision(lower),
		ImageGeneration: hasImageGeneration(lower),
		Audio:           hasAudio(lower),
		Code:            hasCode(lower),
		Embedding:       hasEmbedding(lower),
	}
}

// HasAnyCapability returns true if at least one capability flag is set.
func (c ModelCapabilities) HasAnyCapability() bool {
	return c.Reasoning || c.Vision || c.ImageGeneration || c.Audio || c.Code || c.Embedding
}

// ToMap converts capabilities to a map[string]bool for JSON serialization.
// Only true flags are included to keep the JSONB payload compact.
func (c ModelCapabilities) ToMap() map[string]bool {
	m := make(map[string]bool, 6)
	if c.Reasoning {
		m["reasoning"] = true
	}
	if c.Vision {
		m["vision"] = true
	}
	if c.ImageGeneration {
		m["image_generation"] = true
	}
	if c.Audio {
		m["audio"] = true
	}
	if c.Code {
		m["code"] = true
	}
	if c.Embedding {
		m["embedding"] = true
	}
	return m
}

// --- Internal substring checkers ---

func hasReasoning(lower string) bool {
	return containsAny(lower,
		"o1-", "o3-", "o4-",   // OpenAI o-series
		"-r1", "r1-",          // DeepSeek-R1 and variants
		"thinking",            // QwQ-32B-Preview, etc.
		"reasoning",           // explicit reasoning models
		"nemotron",            // NVIDIA reasoning
		"qwq",                 // Qwen reasoning
		"deepthink",           // DeepThink variants
		"reflection",          // Reflection-70B
	)
}

func hasVision(lower string) bool {
	return containsAny(lower,
		"vision",              // generic vision suffix
		"llava",               // LLaVA family
		"pixtral",             // Mistral vision
		"gpt-4o",              // GPT-4o is multimodal
		"claude-3",            // Claude 3 family supports vision
		"gemini",              // Gemini is multimodal
		"qvq",                 // Qwen VQA
		"-vl",                 // InternVL, Qwen-VL style
		"vl-",                 // VL prefix
		"bakllava",            // BakLLaVA
		"cogvlm",              // CogVLM
		"moondream",           // Moondream
		"idefics",             // IDEFICS
		"minicpm-v",           // MiniCPM-V
		"phi-3-vision",        // Phi-3 Vision
		"internvl",            // InternVL
	)
}

func hasImageGeneration(lower string) bool {
	return containsAny(lower,
		"dall-e",              // OpenAI DALL-E
		"stable-diffusion",   // Stability AI
		"flux",                // Black Forest Labs FLUX
		"midjourney",          // MidJourney
		"imagen",              // Google Imagen
		"sdxl",                // SDXL variants
		"wanx",                // Alibaba Wanx
		"kolors",              // Kolors
		"playground",          // Playground AI
		"dreamshaper",         // DreamShaper
		"sd-",                 // SD-prefix models
	)
}

func hasAudio(lower string) bool {
	return containsAny(lower,
		"whisper",             // OpenAI Whisper transcription
		"tts",                 // Text-to-speech
		"audio",               // Generic audio
		"speech",              // Speech processing
		"voix",                // Voix TTS
		"eleven",              // ElevenLabs style
		"bark",                // Bark TTS
		"coqui",               // Coqui TTS
	)
}

func hasCode(lower string) bool {
	return containsAny(lower,
		"codex",               // OpenAI Codex
		"deepseek-coder",      // DeepSeek Coder
		"qwen-coder",          // Qwen Coder
		"starcoder",           // StarCoder family
		"magicoder",           // MagicCoder
		"codellama",           // Code Llama
		"codegemma",           // CodeGemma
		"phind-codellama",     // Phind CodeLlama
		"wizardcoder",         // WizardCoder
		"code-",               // Generic code prefix (code-davinci, etc.)
		"-coder",              // Generic coder suffix
		"santacoder",          // SantaCoder
	)
}

func hasEmbedding(lower string) bool {
	return containsAny(lower,
		"embed",               // Generic embed
		"embedding",           // Generic embedding
		"text-embedding",      // OpenAI text-embedding-*
		"e5-",                 // E5 embedding family
		"bge-",                // BGE embedding family
		"gte-",                // GTE embedding family
		"nomic-embed",         // Nomic Embed
		"instructor",          // Instructor embeddings
		"sentence",            // Sentence transformers
		"all-minilm",          // MiniLM embeddings
		"jina-embed",          // Jina embeddings
	)
}

// containsAny returns true if s contains any of the given substrings.
// Performs linear scan — no allocations, optimised by early return.
func containsAny(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
