package credentials

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// OllamaModelListResponse represents the OpenAI-compatible /v1/models response
// from an Ollama instance. Ollama exposes this endpoint for compatibility with
// OpenAI tooling, returning all installed/cloud models in a standard format.
type OllamaModelListResponse struct {
	Object string `json:"object"`
	Data   []struct {
		ID string `json:"id"`
	} `json:"data"`
}

// DiscoverAndRegisterOllamaModels connects to an Ollama instance's OpenAI-compatible
// REST endpoint, fetches all available models, auto-provisions model pools inside
// PostgreSQL, and binds the supplied credential to all of them.
//
// Multiple Ollama accounts can be registered — if two accounts host the same model
// (e.g., "llama3:8b"), they are grouped into the same pool and the lock-free
// BalancedChannelPool distributes requests across them automatically.
//
// The apiKey is optional — local Ollama instances typically require no authentication.
// When provided, it is sent as a Bearer token (useful for Ollama behind an auth proxy).
func DiscoverAndRegisterOllamaModels(ctx context.Context, db *pgxpool.Pool, vault *Vault, apiKey, baseURL string, weight int) (int, []string, error) {
	// 1. Fetch live models array from the Ollama instance's OpenAI-compatible endpoint
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequestWithContext(ctx, "GET", baseURL+"/models", nil)
	if err != nil {
		return 0, nil, fmt.Errorf("failed to build model discovery request: %w", err)
	}

	// API key is optional for Ollama — only set auth header when provided
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("ollama endpoint connection failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, nil, fmt.Errorf("ollama rejected request with status code: %d", resp.StatusCode)
	}

	var modelList OllamaModelListResponse
	if err := json.NewDecoder(resp.Body).Decode(&modelList); err != nil {
		return 0, nil, fmt.Errorf("failed to parse ollama models json stream: %w", err)
	}

	// Encrypt the credentials prior to database transaction storage.
	// For keyless local Ollama instances, encrypt a placeholder so the NOT NULL
	// constraint on credentials.encrypted_key is satisfied.
	keyToEncrypt := apiKey
	if keyToEncrypt == "" {
		keyToEncrypt = "ollama-no-auth"
	}
	encryptedKey, err := vault.Encrypt(keyToEncrypt)
	if err != nil {
		return 0, nil, fmt.Errorf("vault encryption failed: %w", err)
	}

	var discoveredModels []string

	// 2. Open a transaction block to safely write configuration metrics
	tx, err := db.Begin(ctx)
	if err != nil {
		return 0, nil, err
	}
	defer tx.Rollback(ctx)

	for _, m := range modelList.Data {
		var poolID int

		// Ensure model_pattern contains the required prefix for routing matching logic.
		// We prepend "ollama/" prefix so that they automatically hit the Ollama proxy routes.
		modelPattern := "ollama/" + m.ID

		// Insert or fetch the model pool ID.
		// If another Ollama account already registered this model, the ON CONFLICT
		// clause keeps the existing pool and merely returns its ID — the new
		// credential is then bound to the same pool, enabling multi-account load balancing.
		err = tx.QueryRow(ctx,
			`INSERT INTO model_pools (model_pattern, strategy)
			 VALUES ($1, 'round-robin')
			 ON CONFLICT (model_pattern) DO UPDATE SET model_pattern = EXCLUDED.model_pattern
			 RETURNING id`,
			modelPattern,
		).Scan(&poolID)

		if err != nil {
			return 0, nil, fmt.Errorf("failed to upsert model pool row for %s: %w", modelPattern, err)
		}

		// Connect the discovered model pool directly to our Ollama instance credential
		_, err = tx.Exec(ctx,
			`INSERT INTO credentials (pool_id, provider, encrypted_key, base_url, weight, is_healthy)
			 VALUES ($1, 'ollama', $2, $3, $4, true)
			 ON CONFLICT DO NOTHING`, // prevents duplicates if the same account is submitted twice
			poolID, encryptedKey, baseURL, weight,
		)
		if err != nil {
			return 0, nil, fmt.Errorf("failed to bind credential to pool %s: %w", modelPattern, err)
		}

		discoveredModels = append(discoveredModels, modelPattern)
	}

	// 3. Emit PostgreSQL NOTIFY channel statement to instantly trigger SyncManager cache swap
	_, err = tx.Exec(ctx, "NOTIFY config_change, 'model_pools:reload'")
	if err != nil {
		return 0, nil, fmt.Errorf("failed to broadcast config change notification: %w", err)
	}

	return len(discoveredModels), discoveredModels, tx.Commit(ctx)
}
