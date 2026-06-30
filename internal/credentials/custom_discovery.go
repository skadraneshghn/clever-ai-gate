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

// OpenAIModelListResponse represents the standard OpenAI /v1/models response.
// Any provider that speaks the OpenAI API format returns this exact shape.
type OpenAIModelListResponse struct {
	Object string `json:"object"`
	Data   []struct {
		ID      string `json:"id"`
		OwnedBy string `json:"owned_by,omitempty"`
	} `json:"data"`
}

// DiscoverAndRegisterCustomModels connects to any OpenAI-compatible provider,
// validates the API key, discovers all available models, and registers them
// into the gateway's routing infrastructure.
//
// Key design: models are stored under their RAW name (no prefix). This means
// if multiple providers (OpenAI, Together, DeepInfra) host the same model
// (e.g. "meta-llama/Llama-3-8b"), they all land in the SAME pool and the
// BalancedChannelPool distributes traffic + does failover automatically.
//
// The provider label is stored in the credentials table so admins can
// distinguish between "together", "deepinfra", "vllm", etc. in the dashboard.
func DiscoverAndRegisterCustomModels(ctx context.Context, db *pgxpool.Pool, vault *Vault, apiKey, baseURL, providerLabel string, weight int) (int, []string, error) {
	if apiKey == "" {
		return 0, nil, fmt.Errorf("api_key is required for OpenAI-compatible provider discovery")
	}

	// Normalise the base URL: trim trailing slash and /v1 suffix so we can
	// cleanly append /v1/models without doubling.
	base := strings.TrimRight(baseURL, "/")
	cleanBase := strings.TrimSuffix(base, "/v1")

	// 1. Validate the key by fetching the models list
	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequestWithContext(ctx, "GET", cleanBase+"/v1/models", nil)
	if err != nil {
		return 0, nil, fmt.Errorf("failed to build model discovery request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("provider endpoint connection failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return 0, nil, fmt.Errorf("API key validation failed: provider returned %d — check your key", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return 0, nil, fmt.Errorf("provider rejected request with status code: %d", resp.StatusCode)
	}

	var modelList OpenAIModelListResponse
	if err := json.NewDecoder(resp.Body).Decode(&modelList); err != nil {
		return 0, nil, fmt.Errorf("failed to parse models response — is this an OpenAI-compatible endpoint? error: %w", err)
	}

	if len(modelList.Data) == 0 {
		return 0, nil, fmt.Errorf("provider returned 0 models — verify your API key has access to models")
	}

	// Normalise the provider label for storage
	if providerLabel == "" {
		providerLabel = "custom"
	}
	providerLabel = strings.ToLower(strings.TrimSpace(providerLabel))

	// Encrypt the API key
	encryptedKey, err := vault.Encrypt(apiKey)
	if err != nil {
		return 0, nil, fmt.Errorf("vault encryption failed: %w", err)
	}

	var discoveredModels []string

	// 2. Open a transaction to safely register all models
	tx, err := db.Begin(ctx)
	if err != nil {
		return 0, nil, err
	}
	defer tx.Rollback(ctx)

	for _, m := range modelList.Data {
		if m.ID == "" {
			continue
		}

		var poolID int

		// Store models under their RAW name — no prefix.
		// This enables automatic cross-provider load balancing:
		// if OpenAI and Together both host "gpt-4o", they share the same pool.
		modelPattern := m.ID

		// Upsert the model pool. ON CONFLICT keeps the existing pool and
		// returns its ID so the new credential joins the same routing ring.
		err = tx.QueryRow(ctx,
			`INSERT INTO model_pools (model_pattern, strategy)
			 VALUES ($1, 'round-robin')
			 ON CONFLICT (model_pattern) DO UPDATE SET model_pattern = EXCLUDED.model_pattern
			 RETURNING id`,
			modelPattern,
		).Scan(&poolID)

		if err != nil {
			return 0, nil, fmt.Errorf("failed to upsert model pool for %s: %w", modelPattern, err)
		}

		// Bind the credential to the pool.
		// ON CONFLICT prevents duplicates if the same key is submitted twice.
		_, err = tx.Exec(ctx,
			`INSERT INTO credentials (pool_id, provider, encrypted_key, base_url, weight, is_healthy)
			 VALUES ($1, $2, $3, $4, $5, true)
			 ON CONFLICT DO NOTHING`,
			poolID, providerLabel, encryptedKey, baseURL, weight,
		)
		if err != nil {
			return 0, nil, fmt.Errorf("failed to bind credential to pool %s: %w", modelPattern, err)
		}

		discoveredModels = append(discoveredModels, modelPattern)
	}

	// 3. Trigger instant cache reload across all gateway instances
	_, err = tx.Exec(ctx, "NOTIFY config_change, 'model_pools:reload'")
	if err != nil {
		return 0, nil, fmt.Errorf("failed to broadcast config change notification: %w", err)
	}

	return len(discoveredModels), discoveredModels, tx.Commit(ctx)
}
