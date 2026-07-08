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

// OllamaTagsResponse represents the native Ollama /api/tags response.
// Used when connecting to Ollama Cloud (https://ollama.com) or any Ollama
// instance that exposes the native REST API.
type OllamaTagsResponse struct {
	Models []struct {
		Name  string `json:"name"`
		Model string `json:"model"`
	} `json:"models"`
}

// OllamaModelListResponse represents the OpenAI-compatible /v1/models response
// from a local Ollama instance. Local Ollama exposes this endpoint for
// compatibility with OpenAI tooling, returning all installed models.
type OllamaModelListResponse struct {
	Object string `json:"object"`
	Data   []struct {
		ID string `json:"id"`
	} `json:"data"`
}

// isOllamaCloud returns true when the base URL points to the official Ollama
// Cloud API (https://ollama.com). This drives which discovery endpoint and
// which proxy path transformer is used.
func isOllamaCloud(baseURL string) bool {
	return strings.Contains(baseURL, "ollama.com")
}

// DiscoverAndRegisterOllamaModels connects to an Ollama instance, fetches all
// available models, auto-provisions model pools in PostgreSQL, and binds the
// supplied credential to all of them.
//
// Two discovery strategies are used based on the base URL:
//
//   - Ollama Cloud (https://ollama.com): hits GET /api/tags with a Bearer
//     token. An API key is required for cloud access.
//
//   - Local Ollama (http://localhost:11434 or custom): hits GET /v1/models
//     (OpenAI-compatible endpoint). API key is optional.
//
// Multiple Ollama accounts can be registered — if two accounts host the same
// model, they are grouped into the same pool and the lock-free
// BalancedChannelPool distributes requests across them automatically.
func DiscoverAndRegisterOllamaModels(ctx context.Context, db *pgxpool.Pool, vault *Vault, apiKey, baseURL string, weight int) (int, []string, error) {
	cloud := isOllamaCloud(baseURL)

	// Ollama Cloud requires an API key; local instances do not.
	if cloud && apiKey == "" {
		return 0, nil, fmt.Errorf("ollama cloud requires an API key — set Authorization: Bearer YOUR_API_KEY")
	}

	client := &http.Client{Timeout: 15 * time.Second}

	var modelIDs []string
	var err error

	if cloud {
		modelIDs, err = discoverOllamaCloudModels(ctx, client, apiKey, baseURL)
	} else {
		modelIDs, err = discoverLocalOllamaModels(ctx, client, apiKey, baseURL)
	}
	if err != nil {
		return 0, nil, err
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

	// Find which pools already have this apiKey bound to avoid duplicates.
	alreadyBound := make(map[int]bool)
	rows, err := db.Query(ctx, `SELECT pool_id, encrypted_key FROM credentials WHERE provider = $1 AND base_url = $2`, "ollama", baseURL)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var poolID int
			var encKey string
			if err := rows.Scan(&poolID, &encKey); err == nil {
				decrypted, decErr := vault.Decrypt(encKey)
				if decErr == nil && decrypted == keyToEncrypt {
					alreadyBound[poolID] = true
				}
			}
		}
	}

	// Open a transaction block to safely write configuration
	tx, err := db.Begin(ctx)
	if err != nil {
		return 0, nil, err
	}
	defer tx.Rollback(ctx)

	for _, modelID := range modelIDs {
		// Register each model under two pool patterns:
		//
		//   1. Prefixed form  "ollama/X" — used by clients that select Ollama
		//      explicitly. The handler.go isOllama block detects this prefix and
		//      strips it from the JSON body before forwarding to the Ollama API.
		//
		//   2. Clean form     "X"         — required for client tools (Cline,
		//      LobeChat, Open WebUI …) that hardcode model name whitelists and
		//      reject unknown prefixes. When the clean form is used the handler
		//      skips the isOllama body-rewrite, but that is correct: the JSON
		//      body already contains the clean model ID that Ollama expects.
		patterns := []string{"ollama/" + modelID, modelID}

		for _, modelPattern := range patterns {
			var poolID int

			// Classify capabilities from the model name
			caps := ClassifyModel(modelPattern)
			capsJSON, err := json.Marshal(caps.ToMap())
			if err != nil {
				capsJSON = []byte("{}")
			}

			// Insert or fetch the model pool ID, updating capabilities on conflict.
			// If another Ollama account already registered this model, the ON CONFLICT
			// clause updates capabilities and returns the existing pool ID — the new
			// credential is then bound to the same pool for multi-account load balancing.
			err = tx.QueryRow(ctx,
				`INSERT INTO model_pools (model_pattern, strategy, capabilities)
				 VALUES ($1, 'round-robin', $2)
				 ON CONFLICT (model_pattern) DO UPDATE
				 SET capabilities = EXCLUDED.capabilities
				 RETURNING id`,
				modelPattern, capsJSON,
			).Scan(&poolID)
			if err != nil {
				return 0, nil, fmt.Errorf("failed to upsert model pool row for %s: %w", modelPattern, err)
			}

			// Connect the discovered model pool directly to our Ollama instance credential
			if !alreadyBound[poolID] {
				_, err = tx.Exec(ctx,
					`INSERT INTO credentials (pool_id, provider, encrypted_key, base_url, weight, is_healthy)
					 VALUES ($1, 'ollama', $2, $3, $4, true)`,
					poolID, encryptedKey, baseURL, weight,
				)
				if err != nil {
					return 0, nil, fmt.Errorf("failed to bind credential to pool %s: %w", modelPattern, err)
				}
				alreadyBound[poolID] = true
			}

			discoveredModels = append(discoveredModels, modelPattern)
		}
	}

	// Emit PostgreSQL NOTIFY to instantly trigger SyncManager cache swap
	_, err = tx.Exec(ctx, "NOTIFY config_change, 'model_pools:reload'")
	if err != nil {
		return 0, nil, fmt.Errorf("failed to broadcast config change notification: %w", err)
	}

	return len(discoveredModels), discoveredModels, tx.Commit(ctx)
}

// discoverOllamaCloudModels fetches models from the official Ollama Cloud API
// using the native GET /api/tags endpoint with Bearer token authentication.
func discoverOllamaCloudModels(ctx context.Context, client *http.Client, apiKey, baseURL string) ([]string, error) {
	base := strings.TrimRight(baseURL, "/")
	req, err := http.NewRequestWithContext(ctx, "GET", base+"/api/tags", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build ollama cloud discovery request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama cloud endpoint connection failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama cloud rejected request with status code: %d", resp.StatusCode)
	}

	var tagsList OllamaTagsResponse
	if err := json.NewDecoder(resp.Body).Decode(&tagsList); err != nil {
		return nil, fmt.Errorf("failed to parse ollama cloud /api/tags response: %w", err)
	}

	if len(tagsList.Models) == 0 {
		return nil, fmt.Errorf("ollama cloud returned no models — verify your API key and account access")
	}

	ids := make([]string, 0, len(tagsList.Models))
	for _, m := range tagsList.Models {
		// Use the canonical "name" field (e.g. "llama4", "gpt-oss:120b")
		name := m.Name
		if name == "" {
			name = m.Model
		}
		if name != "" {
			ids = append(ids, name)
		}
	}
	return ids, nil
}

// discoverLocalOllamaModels fetches models from a local (or self-hosted) Ollama
// instance using the OpenAI-compatible GET /v1/models endpoint.
// An API key is optional — only sent when provided.
func discoverLocalOllamaModels(ctx context.Context, client *http.Client, apiKey, baseURL string) ([]string, error) {
	base := strings.TrimRight(baseURL, "/")
	// Strip trailing /v1 so we can cleanly append /v1/models without doubling
	cleanBase := strings.TrimSuffix(base, "/v1")
	req, err := http.NewRequestWithContext(ctx, "GET", cleanBase+"/v1/models", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build local ollama discovery request: %w", err)
	}

	// API key is optional for local Ollama — only set auth header when provided
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama endpoint connection failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama rejected request with status code: %d", resp.StatusCode)
	}

	var modelList OllamaModelListResponse
	if err := json.NewDecoder(resp.Body).Decode(&modelList); err != nil {
		return nil, fmt.Errorf("failed to parse ollama models json: %w", err)
	}

	ids := make([]string, 0, len(modelList.Data))
	for _, m := range modelList.Data {
		if m.ID != "" {
			ids = append(ids, m.ID)
		}
	}
	return ids, nil
}
