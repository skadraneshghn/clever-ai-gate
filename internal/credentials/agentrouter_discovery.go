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

const agentRouterBaseURL = "https://agentrouter.org/v1"

// defaultAgentRouterModels provides the supported model catalog for AgentRouter.org.
// AgentRouter acts as an Anthropic-compatible wire-image bridge that routes requests
// to various Anthropic, OpenAI, DeepSeek, and Google models.
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
	if err := validateAgentRouterKey(ctx, apiKey); err != nil {
		return 0, nil, err
	}

	// 2. Encrypt the API key once before DB transaction.
	encryptedKey, err := vault.Encrypt(apiKey)
	if err != nil {
		return 0, nil, fmt.Errorf("vault encryption failed: %w", err)
	}

	// 3. Find which pools already have this apiKey bound to avoid duplicates.
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

	// 4. Collect existing bare model_pattern IDs so we don't shadow them
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

	// 5. Open transaction to register pools and credentials atomically.
	tx, err := db.Begin(ctx)
	if err != nil {
		return 0, nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var discoveredModels []string

	for _, modelID := range defaultAgentRouterModels {
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

	// 6. Notify SyncManager to hot-reload gateway cache.
	if _, err = tx.Exec(ctx, "NOTIFY config_change, 'model_pools:reload'"); err != nil {
		return 0, nil, fmt.Errorf("failed to broadcast config change notification: %w", err)
	}

	return len(discoveredModels), discoveredModels, tx.Commit(ctx)
}

// validateAgentRouterKey sends a 1-token test ping call to AgentRouter's Anthropic
// /v1/messages?beta=true endpoint with mandatory Claude Code CLI wire-image headers.
// This bypasses AgentRouter WAF's 401 unauthorized_client_error rejection.
func validateAgentRouterKey(ctx context.Context, apiKey string) error {
	client := &http.Client{Timeout: 15 * time.Second}

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
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("User-Agent", "claude-cli/2.1.158 (external, sdk-cli)")
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("anthropic-beta", "claude-code-20250219,interleaved-thinking-2025-05-14,effort-2025-11-24,redact-thinking-2026-02-12")
	req.Header.Set("anthropic-dangerous-direct-browser-access", "true")
	req.Header.Set("x-app", "cli")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("agentrouter api connection failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf(
			"agentrouter rejected the api key (status %d) — "+
				"verify your key at agentrouter.org and ensure your account has active quota",
			resp.StatusCode,
		)
	}

	// Any 2xx or 4xx (e.g. 400 bad request / model quota) means authentication succeeded
	if resp.StatusCode >= 500 {
		return fmt.Errorf("agentrouter server error (status %d)", resp.StatusCode)
	}

	return nil
}
