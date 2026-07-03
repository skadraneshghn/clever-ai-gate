package proxy

import (
	"bytes"

	"github.com/buger/jsonparser"
)

// sarvamUnsupportedFields lists standard OpenAI Chat Completions request fields
// that are NOT part of Sarvam AI's CreateChatCompletionRequest schema. Sarvam's
// backend rejects unknown top-level fields with HTTP 422 Unprocessable Entity.
//
// The gateway's total-exhaustion policy treats 422 as a credential-auth error
// (isCredentialAuthError) and penalises the key for 20–30 minutes before
// rotating — even though every key is healthy. Stripping these fields upstream
// keeps credentials healthy and lets any OpenAI-compatible client use Sarvam.
var sarvamUnsupportedFields = []string{
	"stream_options",      // OpenAI streaming usage control — very common in streaming clients
	"logprobs",            // OpenAI log probabilities
	"top_logprobs",        // OpenAI log probabilities
	"service_tier",        // OpenAI priority tier
	"store",               // OpenAI conversation storage
	"metadata",            // OpenAI request metadata
	"parallel_tool_calls", // OpenAI parallel function calling
	"user",                // OpenAI end-user identifier
	"logit_bias",          // OpenAI token bias
	"suffix",              // OpenAI completion suffix
}

// sanitizeSarvamRequest removes OpenAI-only request fields that Sarvam's strict
// schema rejects. It performs a fast presence probe (cheap byte scans) and only
// rewrites the body when at least one unsupported field is actually present,
// keeping the common clean-request path allocation-free.
//
// This runs for BOTH routing forms (prefixed "sarvam/..." and clean alias such
// as "sarvam-105b") because it is invoked from the hot-path gated on the
// resolved credential's provider, not on the model prefix.
//
// jsonparser.Delete returns the original buffer when the key is absent, so the
// deletion loop is safe to run once the probe has fired. bytes.Contains on the
// (text-only) chat body is cheap and mirrors the existing full-body scan in
// ExtractStreamFlag.
func sanitizeSarvamRequest(body []byte) []byte {
	// Fast probe: skip the deletion pass entirely when no unsupported field is
	// present.
	needed := false
	for _, f := range sarvamUnsupportedFields {
		if bytes.Contains(body, []byte(`"`+f+`"`)) {
			needed = true
			break
		}
	}
	if !needed {
		return body
	}

	out := body
	for _, f := range sarvamUnsupportedFields {
		if bytes.Contains(out, []byte(`"`+f+`"`)) {
			out = jsonparser.Delete(out, f)
		}
	}
	return out
}
