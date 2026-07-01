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

const openRouterBaseURL = "https://openrouter.ai/api/v1"

// openRouterModelListResponse maps the OpenRouter GET /api/v1/models JSON envelope.
// OpenRouter returns the full catalog (all tiers); we filter for free models locally.
type openRouterModelListResponse struct {
	Data []openRouterModel `json:"data"`
}

// openRouterModel represents a single model entry from the OpenRouter catalog.
// We only read the fields we need for discovery and filtering.
type openRouterModel struct {
	// ID is the canonical routing slug, e.g. "deepseek/deepseek-r1:free".
	// Free-tier models ALWAYS end with the ":free" suffix.
	ID   string `json:"id"`
	Name string `json:"name"`

	// Pricing holds per-token costs as decimal strings.
	// Free models have prompt="0" and completion="0".
	Pricing struct {
		Prompt     string `json:"prompt"`
		Completion string `json:"completion"`
	} `json:"pricing"`
}

// isFreeOpenRouterModel returns true when the model is a free-tier offering.
// The ":free" ID suffix is the canonical and most reliable signal — it is applied
// by OpenRouter itself and never appears on paid variants of the same model.
// Zero pricing is checked as a secondary guard to future-proof the filter.
func isFreeOpenRouterModel(m openRouterModel) bool {
	// Primary: OpenRouter's own naming convention for free access
	if strings.HasSuffix(strings.ToLower(m.ID), ":free") {
		return true
	}
	// Secondary: explicit zero pricing on both prompt and completion tokens
	if m.Pricing.Prompt == "0" && m.Pricing.Completion == "0" {
		return true
	}
	return false
}

// DiscoverAndRegisterOpenRouterModels fetches the full OpenRouter model catalog,
// filters for ONLY free-tier models (`:free` suffix or zero pricing), auto-provisions
// model_pools in PostgreSQL for each one, and binds the provided API key credential
// to all of them in a single atomic transaction.
//
// This mirrors the pattern established by DiscoverAndRegisterNvidiaModels and
// DiscoverAndRegisterOllamaModels for architectural consistency.
//
// Returns the count of registered models, their IDs, and any error encountered.
func DiscoverAndRegisterOpenRouterModels(ctx context.Context, db *pgxpool.Pool, vault *Vault, apiKey string, weight int) (int, []string, error) {
	if apiKey == "" {
		return 0, nil, fmt.Errorf("openrouter api key is required for model discovery")
	}

	// 1. Fetch the full OpenRouter model catalog
	models, err := fetchOpenRouterModels(ctx, apiKey)
	if err != nil {
		return 0, nil, err
	}

	// 2. Encrypt the API key once before the database loop (save CPU on hot path)
	encryptedKey, err := vault.Encrypt(apiKey)
	if err != nil {
		return 0, nil, fmt.Errorf("vault encryption failed: %w", err)
	}

	// 3. Open a transaction to atomically write all pools and credentials
	tx, err := db.Begin(ctx)
	if err != nil {
		return 0, nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck — intentional deferred cleanup

	var discoveredModels []string

	for _, m := range models {
		// 4. Apply strict free-tier filter — skip all premium models
		if !isFreeOpenRouterModel(m) {
			continue
		}

		modelPattern := m.ID // Use as-is: OpenRouter routes on the full slug

		// 5. Classify capabilities from the model identifier
		caps := ClassifyModel(modelPattern)
		capsJSON, err := json.Marshal(caps.ToMap())
		if err != nil {
			capsJSON = []byte("{}")
		}

		// 6. Upsert the model_pool — updates capabilities on re-discovery
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

		// 7. Bind the OpenRouter credential to this pool (idempotent via ON CONFLICT)
		_, err = tx.Exec(ctx,
			`INSERT INTO credentials (pool_id, provider, encrypted_key, base_url, weight, is_healthy)
			 VALUES ($1, 'openrouter', $2, $3, $4, true)
			 ON CONFLICT DO NOTHING`,
			poolID, encryptedKey, openRouterBaseURL, weight,
		)
		if err != nil {
			return 0, nil, fmt.Errorf("failed to bind credential for pool %s: %w", modelPattern, err)
		}

		discoveredModels = append(discoveredModels, modelPattern)
	}

	// 8. Notify the SyncManager to instantly hot-reload the routing cache
	if _, err = tx.Exec(ctx, "NOTIFY config_change, 'model_pools:reload'"); err != nil {
		return 0, nil, fmt.Errorf("failed to broadcast config change notification: %w", err)
	}

	return len(discoveredModels), discoveredModels, tx.Commit(ctx)
}

// fetchOpenRouterModels calls GET /api/v1/models on the OpenRouter API and returns
// the full model catalog. Authentication uses a Bearer token (required by OpenRouter).
func fetchOpenRouterModels(ctx context.Context, apiKey string) ([]openRouterModel, error) {
	client := &http.Client{Timeout: 20 * time.Second}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, openRouterBaseURL+"/models", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build openrouter discovery request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("HTTP-Referer", "https://github.com/skadraneshghn/clever-ai-gate")
	req.Header.Set("X-Title", "Clever AI Gate")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openrouter api connection failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("openrouter rejected the api key (401 unauthorized) — verify your key at openrouter.ai/settings/keys")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("openrouter models endpoint returned unexpected status: %d", resp.StatusCode)
	}

	var catalog openRouterModelListResponse
	if err := json.NewDecoder(resp.Body).Decode(&catalog); err != nil {
		return nil, fmt.Errorf("failed to parse openrouter model catalog: %w", err)
	}

	if len(catalog.Data) == 0 {
		return nil, fmt.Errorf("openrouter returned an empty model catalog — check your api key permissions")
	}

	return catalog.Data, nil
}
