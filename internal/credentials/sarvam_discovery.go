package credentials

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// DiscoverAndRegisterSarvamModels validates the Sarvam AI API key, then iterates
// over the static model manifest to auto-provision model_pools and bind the
// credential to every Sarvam chat model (sarvam-30b, sarvam-105b) in a single
// atomic transaction.
//
// Sarvam AI is natively OpenAI-compatible (POST /v1/chat/completions, SSE
// streaming, reasoning_content deltas). It does NOT expose a /v1/models
// endpoint — the chat model enum is fixed — so the full catalog is maintained
// as a hardcoded manifest (sarvam_manifest.go). Key validity is confirmed via a
// minimal chat request before registration begins.
//
// Authentication note: Sarvam returns HTTP 403 (not 401) for invalid keys; the
// gateway's isCredentialAuthError already detects 403 to trigger lock-free
// token rotation at runtime.
//
// Returns the count of registered model aliases, their patterns, and any error.
func DiscoverAndRegisterSarvamModels(ctx context.Context, db *pgxpool.Pool, vault *Vault, apiKey string, weight int) (int, []string, error) {
	if apiKey == "" {
		return 0, nil, fmt.Errorf("sarvam api key is required for model discovery")
	}

	if weight <= 0 {
		weight = 1
	}

	// 1. Validate the API key with a minimal chat request.
	if err := validateSarvamKey(ctx, apiKey); err != nil {
		return 0, nil, err
	}

	// 2. Encrypt the API key once before the database loop (save CPU on hot path).
	encryptedKey, err := vault.Encrypt(apiKey)
	if err != nil {
		return 0, nil, fmt.Errorf("vault encryption failed: %w", err)
	}

	// 2.5 Find which pools already have this apiKey bound to avoid duplicates.
	alreadyBound := make(map[int]bool)
	rows, err := db.Query(ctx, `SELECT pool_id, encrypted_key FROM credentials WHERE provider = $1`, "sarvam")
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var poolID int
			var encKey string
			if err := rows.Scan(&poolID, &encKey); err == nil {
				decrypted, decErr := vault.Decrypt(encKey)
				if decErr == nil && decrypted == apiKey {
					alreadyBound[poolID] = true
				}
			}
		}
	}

	// 3. Open a transaction to atomically write all pools and credentials.
	tx, err := db.Begin(ctx)
	if err != nil {
		return 0, nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck — intentional deferred cleanup

	var discoveredModels []string

	manifest := SarvamManifest()

	for _, entry := range manifest {
		// Each manifest entry is registered under two pool patterns:
		//
		//   1. The prefixed gateway pattern (e.g. "sarvam/sarvam-105b") — used by
		//      clients that explicitly select the Sarvam provider. The handler.go
		//      isSarvam block detects this prefix and strips it from the JSON
		//      body before forwarding to Sarvam.
		//   2. The clean upstream model name (e.g. "sarvam-105b") — required for
		//      client tools (Cline, LobeChat, Open WebUI …) that hardcode a
		//      whitelist of known model names and reject any prefixed variant.
		//
		// Both pools bind the same encrypted Sarvam credential, so the
		// load-balancer ring treats them independently.
		patterns := []string{entry.Pattern}
		if entry.Model != entry.Pattern {
			patterns = append(patterns, entry.Model)
		}

		for _, modelPattern := range patterns {
			// 4. Classify capabilities from the model identifier, then apply
			//    Sarvam-specific overrides. The heuristic classifier does not
			//    recognise "sarvam" as a reasoning/code family, so the overrides
			//    are mandatory for accurate capability metadata.
			caps := ClassifyModel(modelPattern)
			applySarvamOverrides(&caps)

			capsJSON, err := json.Marshal(caps.ToMap())
			if err != nil {
				capsJSON = []byte("{}")
			}

			// 5. Upsert the model_pool — updates capabilities on re-discovery.
			var poolID int
			err = tx.QueryRow(ctx,
				`INSERT INTO model_pools (model_pattern, strategy, capabilities)
				 VALUES ($1, 'round-robin', $2)
				 ON CONFLICT (model_pattern) DO UPDATE
				 SET capabilities = EXCLUDED.capabilities
				 RETURNING id`,
				modelPattern, capsJSON,
			).Scan(&poolID)
			if err != nil {
				return 0, nil, fmt.Errorf("failed to upsert model pool for %s: %w", modelPattern, err)
			}

			// 6. Bind the Sarvam credential to this pool (idempotent).
			if !alreadyBound[poolID] {
				_, err = tx.Exec(ctx,
					`INSERT INTO credentials (pool_id, provider, encrypted_key, base_url, weight, is_healthy)
					 VALUES ($1, 'sarvam', $2, $3, $4, true)`,
					poolID, encryptedKey, SarvamBaseURL, weight,
				)
				if err != nil {
					return 0, nil, fmt.Errorf("failed to bind credential for pool %s: %w", modelPattern, err)
				}
				alreadyBound[poolID] = true
			}

			discoveredModels = append(discoveredModels, modelPattern)
		}
	}

	// 7. Notify the SyncManager to instantly hot-reload the routing cache.
	if _, err = tx.Exec(ctx, "NOTIFY config_change, 'model_pools:reload'"); err != nil {
		return 0, nil, fmt.Errorf("failed to broadcast config change notification: %w", err)
	}

	return len(discoveredModels), discoveredModels, tx.Commit(ctx)
}

// validateSarvamKey sends a minimal chat request to Sarvam AI to confirm the
// API key is valid. Sarvam has no dedicated key-check endpoint, so a one-token
// chat completion (reasoning disabled) is the cheapest reliable probe.
//
// Sarvam returns HTTP 403 (not 401) for invalid/missing keys. Only 403/401
// definitively reject the key and block registration. Any other non-200 status
// (429 rate-limited, 5xx upstream busy, …) is treated as inconclusive: the key
// is probably valid, so registration proceeds — the runtime 403-detection and
// lock-free rotation remain the ultimate safety net for any actually-bad key.
func validateSarvamKey(ctx context.Context, apiKey string) error {
	client := &http.Client{Timeout: 15 * time.Second}

	// max_tokens=1 and reasoning_effort=null keep the probe cost near-zero
	// (Sarvam enables reasoning by default, which would otherwise consume tokens).
	body := `{"model":"sarvam-30b","messages":[{"role":"user","content":"hi"}],"max_tokens":1,"reasoning_effort":null}`

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		SarvamBaseURL+"/v1/chat/completions", strings.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to build validation request: %w", err)
	}

	// Sarvam accepts both api-subscription-key and Authorization: Bearer on all
	// endpoints. Send both for bulletproof auth (mirrors the rewriter).
	req.Header.Set("api-subscription-key", apiKey)
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("sarvam connection failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("sarvam rejected the api key (%d) — verify your key at dashboard.sarvam.ai", resp.StatusCode)
	}

	// 200 (verified), 429 (rate-limited but valid), 5xx (upstream busy), etc.:
	// the key is not definitively invalid — proceed with registration.
	return nil
}

// applySarvamOverrides sets capability flags for Sarvam AI chat models. The
// heuristic classifier (ClassifyModel) does not recognise the "sarvam" model
// family, so these overrides are mandatory for accurate capability metadata.
//
// Both Sarvam chat models support reasoning (reasoning_effort + reasoning_content
// deltas) and are benchmarked for code generation.
func applySarvamOverrides(caps *ModelCapabilities) {
	caps.Reasoning = true
	caps.Code = true
}
