package credentials

// Centralized provider identity constants — the single source of truth for the
// provider string identifiers used across model discovery, pool management, and
// request routing. Both the pool manager (discovery writes these into the
// credentials table) and the request handshaker (the proxy routing interceptor)
// reference these constants so the credential layer and the routing layer never
// drift apart.
const (
	// ProviderGemini is the credential provider identity persisted on every
	// Google AI Studio API key (base_url https://generativelanguage.googleapis.com).
	// All transpilation, URL, and header logic in the proxy keys off this value:
	// a credential carrying ProviderGemini activates the OpenAI→Gemini body
	// transpiler, the geminiPath URL transformer, and the ?key= query auth.
	ProviderGemini = "gemini"

	// ProviderGeminiLegacy is the namespace token clients prepend to explicitly
	// route to the AI Studio pipeline (e.g. "gemini/gemini-3.5-flash"). In this
	// codebase it is identical to ProviderGemini: the routing prefix token and the
	// stored credential provider identity share the same string, so a request can
	// be normalized to the AI Studio pipeline and bind to a ProviderGemini
	// credential in a single step.
	ProviderGeminiLegacy = "gemini"

	// ProviderGeminiStudio is the canonical routing label for the optimized
	// Google AI Studio pipeline. The request remapping engine in the proxy
	// normalizes both routing forms — the slash-prefixed "gemini/<model>" used by
	// Kilo Code agents and the standalone "gemini-<model>" used by standard chat
	// clients — to this label. The credentials these requests bind to still carry
	// ProviderGemini, which is what actually activates the transpiler; the studio
	// label is the logical routing target, gemini is the credential identity.
	ProviderGeminiStudio = "gemini_studio"
)
