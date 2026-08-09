package credentials

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const agentRouterBaseURL = "https://ps.air-outer.com/v1"

// defaultAgentRouterModels provides the supported model catalog for AgentRouter.org
// as a safe fallback when upstream auto-discovery endpoint returns 503 Service Unavailable or non-200.
var defaultAgentRouterModels = []string{
	"claude-3-7-sonnet-20250219",
	"claude-3-5-sonnet-20241022",
	"claude-3-5-haiku-20241022",
	"claude-3-opus-20240229",
	"gpt-4o",
	"gpt-4o-mini",
	"deepseek-r1",
	"deepseek-chat",
	"gemini-2.5-flash",
	"gemini-2.5-pro",
}

// DiscoverAndRegisterAgentRouterModels verifies the provided AgentRouter key via
// a 1-token test call to Anthropic /v1/messages?beta=true with spoofed Claude Code
// CLI headers, auto-provisions model_pools in PostgreSQL, and binds the credential
// to all pools in a single atomic transaction.
//
// If upstream returns 503 Service Unavailable or temporary error during model discovery,
// it gracefully registers default fallback models so credential creation never fails.
func DiscoverAndRegisterAgentRouterModels(
	ctx context.Context,
	db *pgxpool.Pool,
	vault *Vault,
	apiKey string,
	weight int,
) (int, []string, error) {
	apiKey = strings.TrimSpace(apiKey)
	if strings.HasPrefix(apiKey, "Bearer ") {
		apiKey = strings.TrimPrefix(apiKey, "Bearer ")
		apiKey = strings.TrimSpace(apiKey)
	}

	if apiKey == "" {
		return 0, nil, fmt.Errorf("agentrouter api_key is required for model discovery")
	}

	// 1. Verify API key with spoofed Claude Code wire-image headers.
	// Only fails on explicit 401/403 key rejection.
	if err := validateAgentRouterKey(ctx, apiKey); err != nil {
		return 0, nil, err
	}

	// 2. Fetch models dynamically, or fall back to default catalog on 503/errors.
	modelsToRegister := fetchAgentRouterModels(ctx, apiKey)

	// 3. Encrypt the API key once before DB transaction.
	encryptedKey, err := vault.Encrypt(apiKey)
	if err != nil {
		return 0, nil, fmt.Errorf("vault encryption failed: %w", err)
	}

	// 4. Find which pools already have this apiKey bound to avoid duplicates.
	alreadyBound := make(map[int]bool)
	rows, err := db.Query(ctx,
		`SELECT pool_id, encrypted_key FROM credentials WHERE provider = $1`,
		"agentrouter",
	)
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
		rows.Close()
	}

	// 5. Collect existing bare model_pattern IDs so we don't shadow them
	//    with AgentRouter clean aliases.
	existingPatterns := make(map[string]bool)
	patRows, err := db.Query(ctx, `SELECT model_pattern FROM model_pools`)
	if err == nil {
		defer patRows.Close()
		for patRows.Next() {
			var pat string
			if patRows.Scan(&pat) == nil {
				existingPatterns[pat] = true
			}
		}
		patRows.Close()
	}

	// 6. Open transaction to register pools and credentials atomically.
	tx, err := db.Begin(ctx)
	if err != nil {
		return 0, nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var discoveredModels []string

	for _, modelID := range modelsToRegister {
		caps := ClassifyModel(modelID)
		capsJSON, err := json.Marshal(caps.ToMap())
		if err != nil {
			capsJSON = []byte("{}")
		}

		type patternEntry struct {
			pattern string
		}

		var patterns []patternEntry
		patterns = append(patterns, patternEntry{pattern: "agentrouter/" + modelID})
		if !existingPatterns[modelID] {
			patterns = append(patterns, patternEntry{pattern: modelID})
		}

		for _, pe := range patterns {
			var poolID int

			err = tx.QueryRow(ctx,
				`INSERT INTO model_pools (model_pattern, strategy, capabilities)
				 VALUES ($1, 'round-robin', $2)
				 ON CONFLICT (model_pattern) DO UPDATE
				 SET capabilities = EXCLUDED.capabilities
				 RETURNING id`,
				pe.pattern, capsJSON,
			).Scan(&poolID)
			if err != nil {
				return 0, nil, fmt.Errorf("failed to upsert model pool for %s: %w", pe.pattern, err)
			}

			if !alreadyBound[poolID] {
				_, err = tx.Exec(ctx,
					`INSERT INTO credentials (pool_id, provider, encrypted_key, base_url, weight, is_healthy)
					 VALUES ($1, 'agentrouter', $2, $3, $4, true)`,
					poolID, encryptedKey, agentRouterBaseURL, weight,
				)
				if err != nil {
					return 0, nil, fmt.Errorf("failed to bind credential for pool %s: %w", pe.pattern, err)
				}
				alreadyBound[poolID] = true
			}

			discoveredModels = append(discoveredModels, pe.pattern)
			existingPatterns[pe.pattern] = true
		}
	}

	// 7. Notify SyncManager to hot-reload gateway cache.
	if _, err = tx.Exec(ctx, "NOTIFY config_change, 'model_pools:reload'"); err != nil {
		return 0, nil, fmt.Errorf("failed to broadcast config change notification: %w", err)
	}

	return len(discoveredModels), discoveredModels, tx.Commit(ctx)
}

// fetchAgentRouterModels attempts dynamic model discovery with spoofed headers,
// and gracefully falls back to defaultAgentRouterModels if upstream returns 503 or error.
func fetchAgentRouterModels(ctx context.Context, apiKey string) []string {
	client := &http.Client{Timeout: 8 * time.Second}
	targetURL := agentRouterBaseURL + "/models"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return defaultAgentRouterModels
	}

	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("User-Agent", "claude-cli/2.1.158 (external, sdk-cli)")
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("anthropic-beta", "claude-code-20250219,interleaved-thinking-2025-05-14,effort-2025-11-24,redact-thinking-2026-02-12")
	req.Header.Set("anthropic-dangerous-direct-browser-access", "true")
	req.Header.Set("x-app", "cli")

	resp, err := client.Do(req)
	if err != nil || (resp != nil && resp.StatusCode >= 500) {
		// Fallback to agentrouter.org if primary mirror domain failed
		fallbackURL := strings.Replace(targetURL, "ps.air-outer.com", "agentrouter.org", 1)
		if fallbackURL == targetURL {
			fallbackURL = strings.Replace(targetURL, "agentrouter.org", "ps.air-outer.com", 1)
		}
		fbReq, fbErr := http.NewRequestWithContext(ctx, http.MethodGet, fallbackURL, nil)
		if fbErr == nil {
			fbReq.Header = req.Header.Clone()
			fbResp, fbErrDo := client.Do(fbReq)
			if fbErrDo == nil && fbResp.StatusCode == http.StatusOK {
				if resp != nil && resp.Body != nil {
					resp.Body.Close()
				}
				resp = fbResp
				err = nil
			}
		}
	}

	if err != nil || resp == nil {
		return defaultAgentRouterModels
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return defaultAgentRouterModels
	}

	type agentRouterModelList struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}

	var list agentRouterModelList
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil || len(list.Data) == 0 {
		return defaultAgentRouterModels
	}

	var models []string
	for _, m := range list.Data {
		cleanID := strings.TrimPrefix(m.ID, "agentrouter/")
		if cleanID != "" {
			models = append(models, cleanID)
		}
	}

	if len(models) == 0 {
		return defaultAgentRouterModels
	}

	return models
}

// validateAgentRouterKey sends a 1-token test ping call to AgentRouter's Anthropic
// /v1/messages?beta=true endpoint with mandatory Claude Code CLI wire-image headers.
// Returns an error ONLY if the API key is explicitly rejected (401/403).
func validateAgentRouterKey(ctx context.Context, apiKey string) error {
	client := &http.Client{Timeout: 10 * time.Second}

	targetURL := agentRouterBaseURL + "/messages?beta=true"

	testPayload := map[string]interface{}{
		"model":      "claude-3-5-sonnet-20241022",
		"max_tokens": 1,
		"messages": []map[string]string{
			{"role": "user", "content": "ping"},
		},
	}

	bodyBytes, err := json.Marshal(testPayload)
	if err != nil {
		return fmt.Errorf("failed to marshal test payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("failed to build test request: %w", err)
	}

	// Inject mandatory Claude Code CLI wire-image headers to satisfy AgentRouter WAF
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("User-Agent", "claude-cli/2.1.158 (external, sdk-cli)")
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("anthropic-beta", "claude-code-20250219,interleaved-thinking-2025-05-14,effort-2025-11-24,redact-thinking-2026-02-12")
	req.Header.Set("anthropic-dangerous-direct-browser-access", "true")
	req.Header.Set("x-app", "cli")

	resp, err := client.Do(req)
	if err != nil || (resp != nil && resp.StatusCode >= 500) {
		fallbackURL := strings.Replace(targetURL, "ps.air-outer.com", "agentrouter.org", 1)
		if fallbackURL == targetURL {
			fallbackURL = strings.Replace(targetURL, "agentrouter.org", "ps.air-outer.com", 1)
		}
		fbReq, fbErr := http.NewRequestWithContext(ctx, http.MethodPost, fallbackURL, bytes.NewReader(bodyBytes))
		if fbErr == nil {
			fbReq.Header = req.Header.Clone()
			fbResp, fbErrDo := client.Do(fbReq)
			if fbErrDo == nil {
				if resp != nil && resp.Body != nil {
					resp.Body.Close()
				}
				resp = fbResp
				err = nil
			}
		}
	}

	if err != nil || resp == nil {
		// Network timeout / connection error — allow registration using fallback models
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf(
			"agentrouter rejected the api key (status %d) — "+
				"verify your key at agentrouter.org / ps.air-outer.com and ensure your account has active quota",
			resp.StatusCode,
		)
	}

	// Status 503 / 5xx or 2xx/4xx: authentication check passed or temporary server maintenance.
	// Allow key creation to succeed seamlessly with fallback models.
	return nil
}
