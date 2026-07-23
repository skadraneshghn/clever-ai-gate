package proxy

import (
	"bytes"
	"encoding/json"
	"strings"
)

// jiekouUnsupportedFields lists standard OpenAI Chat Completions fields that
// Jiekou.AI (and the models it proxies) rejects with HTTP 400.
var jiekouUnsupportedFields = []string{
	"logit_bias",          // OpenAI token bias
	"user",                // OpenAI end-user identifier
	"logprobs",            // OpenAI log probabilities
	"top_logprobs",        // OpenAI log probability list
	"stream_options",      // OpenAI streaming usage control
	"service_tier",        // OpenAI priority tier
	"store",               // OpenAI conversation storage
	"metadata",            // OpenAI request metadata
	"parallel_tool_calls", // OpenAI parallel function calling
	"suffix",              // OpenAI completion suffix
}

// jiekouReasoningUnsupportedFields are additional fields that reasoning/beta
// models (gpt-5*, o1, o3, *-sol) reject on top of the base unsupported list.
var jiekouReasoningUnsupportedFields = []string{
	"max_tokens", // reasoning models use max_completion_tokens instead
}

// IsFixedParamReasoningModel returns true when a model enforces fixed generation
// parameters as hard constraints:
//
//	temperature = 1  (cannot be changed)
//	top_p       = 1  (cannot be changed)
//	n           = 1  (cannot be changed)
//	presence_penalty  = 0
//	frequency_penalty = 0
//
// Affected model families (as documented by OpenAI and Jiekou upstream errors):
//   - gpt-5* (gpt-5.6-sol, gpt-5o, gpt-5-mini, …)
//   - *-sol   (gpt-5.6-sol, gpt-4.5-sol, …)
//   - o1*     (o1, o1-mini, o1-preview, o1-pro, …)
//   - o3*     (o3, o3-mini, o3-pro, …)
//   - o4*     (o4-mini, …)
//   - *reasoning* (any future reasoning variant)
//
// The function is intentionally exported so the health-probe adapter can
// use it directly without duplicating the model-family matching logic.
func IsFixedParamReasoningModel(modelName string) bool {
	lower := strings.ToLower(modelName)
	// Strip any surviving gateway prefix before matching
	if idx := strings.LastIndex(lower, "/"); idx >= 0 {
		lower = lower[idx+1:]
	}
	return strings.HasPrefix(lower, "gpt-5") ||
		strings.HasSuffix(lower, "-sol") ||
		strings.HasPrefix(lower, "o1") ||
		strings.HasPrefix(lower, "o3") ||
		strings.HasPrefix(lower, "o4") ||
		strings.Contains(lower, "reasoning")
}

// sanitizeJiekouRequest applies layered transforms to requests destined for Jiekou.AI.
//
// Layer 1 — Universal (all Jiekou models):
//   - Strip "jiekou/" routing prefix from "model" JSON field
//   - Remove OpenAI-only fields that Jiekou passes through to upstream providers
//     which then reject them (logit_bias, user, logprobs, stream_options, …)
//
// Layer 2 — Kimi/Moonshot models (moonshotai/*, kimi-*):
//   - Clamp temperature to [0.0, 1.0]  (Moonshot enforces this range)
//   - Remove invalid top_p values
//
// Layer 3 — Reasoning/Beta GPT models (gpt-5*, *-sol, o1*, o3*, o4*):
//   - ENFORCE temperature=1, top_p=1, n=1, presence_penalty=0, frequency_penalty=0
//     (upstream returns HTTP 400 "beta-limitations" if any differ)
//   - Remap max_tokens → max_completion_tokens (reasoning API requirement)
//
// This sanitizer is called from forwardRequest gated on cred.Provider == "jiekou".
func sanitizeJiekouRequest(body []byte) []byte {
	// Fast probe: skip full parse when nothing can possibly need rewriting.
	// We check for the most common triggers; the full rewrite handles the rest.
	needsRewrite := false
	triggers := [][]byte{
		[]byte(`"jiekou/`),
		[]byte(`"temperature"`),
		[]byte(`"top_p"`),
		[]byte(`"presence_penalty"`),
		[]byte(`"frequency_penalty"`),
		[]byte(`"max_tokens"`),
	}
	for _, t := range triggers {
		if bytes.Contains(body, t) {
			needsRewrite = true
			break
		}
	}
	if !needsRewrite {
		for _, f := range jiekouUnsupportedFields {
			if bytes.Contains(body, []byte(`"`+f+`"`)) {
				needsRewrite = true
				break
			}
		}
	}
	if !needsRewrite {
		return body
	}

	// Full JSON parse — only reached when a rewrite is needed.
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return body // Don't corrupt unparseable body
	}

	// ── Layer 1: Universal ────────────────────────────────────────────────────

	// Strip "jiekou/" routing prefix from the model field.
	// "jiekou/gpt-5.6-sol"        →  "gpt-5.6-sol"
	// "jiekou/moonshotai/kimi-k3" →  "moonshotai/kimi-k3"
	// "gpt-5.6-sol"               →  "gpt-5.6-sol"  (already clean, no-op)
	modelName := ""
	if modelRaw, ok := payload["model"].(string); ok {
		if strings.HasPrefix(modelRaw, "jiekou/") {
			modelRaw = modelRaw[len("jiekou/"):]
			payload["model"] = modelRaw
		}
		modelName = modelRaw
	}

	// Remove unsupported OpenAI-specific fields (applies to all models).
	for _, f := range jiekouUnsupportedFields {
		delete(payload, f)
	}

	// ── Layer 2: Kimi/Moonshot clamp ──────────────────────────────────────────

	isKimiMoonshot := strings.Contains(strings.ToLower(modelName), "kimi") ||
		strings.Contains(strings.ToLower(modelName), "moonshot")

	if isKimiMoonshot {
		if tempRaw, exists := payload["temperature"]; exists {
			if floatVal, ok := toFloat64(tempRaw); ok {
				if floatVal > 1.0 {
					payload["temperature"] = 0.7
				} else if floatVal < 0.0 {
					payload["temperature"] = 0.1
				}
			}
		}
		if topPRaw, exists := payload["top_p"]; exists {
			if floatVal, ok := toFloat64(topPRaw); ok {
				if floatVal > 1.0 || floatVal <= 0.0 {
					delete(payload, "top_p")
				}
			}
		}
	}

	// ── Layer 3: Reasoning/Beta GPT fixed-parameter enforcement ───────────────
	//
	// Models like gpt-5.6-sol, o1, o3, o4-mini enforce:
	//   temperature=1, top_p=1, n=1, presence_penalty=0, frequency_penalty=0
	//
	// The upstream error is explicit:
	//   "this model has beta-limitations, temperature, top_p and n are fixed
	//    at 1, while presence_penalty and frequency_penalty are fixed at 0"
	//
	// We OVERRIDE whatever the client sent rather than clamping, because even
	// a "valid" temperature of 0.7 will cause HTTP 400 for these models.

	if IsFixedParamReasoningModel(modelName) {
		payload["temperature"] = float64(1)
		payload["top_p"] = float64(1)
		payload["n"] = float64(1)
		payload["presence_penalty"] = float64(0)
		payload["frequency_penalty"] = float64(0)

		// Reasoning models use max_completion_tokens, not max_tokens.
		// Migrate the value if present; ignore if max_completion_tokens already set.
		if maxTok, exists := payload["max_tokens"]; exists {
			if _, alreadySet := payload["max_completion_tokens"]; !alreadySet {
				payload["max_completion_tokens"] = maxTok
			}
			delete(payload, "max_tokens")
		}

		// Remove additional fields that reasoning models reject.
		for _, f := range jiekouReasoningUnsupportedFields {
			delete(payload, f)
		}
	} else if !isKimiMoonshot {
		// Standard (non-Kimi, non-reasoning) models: apply general temperature
		// clamping for safety. Jiekou proxies OpenAI-compatible models that all
		// accept temperature ∈ [0.0, 2.0], so this is a broad safety clamp.
		if tempRaw, exists := payload["temperature"]; exists {
			if floatVal, ok := toFloat64(tempRaw); ok {
				if floatVal > 2.0 {
					payload["temperature"] = 1.0
				} else if floatVal < 0.0 {
					payload["temperature"] = 0.0
				}
			}
		}
	}

	out, err := json.Marshal(payload)
	if err != nil {
		return body
	}
	return out
}

// toFloat64 converts common numeric JSON value types to float64.
func toFloat64(val interface{}) (float64, bool) {
	switch v := val.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case json.Number:
		if f, err := v.Float64(); err == nil {
			return f, true
		}
	}
	return 0, false
}
