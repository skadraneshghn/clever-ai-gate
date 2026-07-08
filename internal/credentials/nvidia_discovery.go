package credentials

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type NvidiaModelListResponse struct {
	Object string `json:"object"`
	Data   []struct {
		ID string `json:"id"`
	} `json:"data"`
}

// DiscoverAndRegisterNvidiaModels connects to NVIDIA's REST endpoint, fetches all available models,
// auto-provisions model pools inside PostgreSQL, and binds the newly added key to all of them.
func DiscoverAndRegisterNvidiaModels(ctx context.Context, db *pgxpool.Pool, vault *Vault, apiKey, baseURL string, weight int) (int, []string, error) {
	// 1. Fetch live models array directly from NVIDIA
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequestWithContext(ctx, "GET", baseURL+"/models", nil)
	if err != nil {
		return 0, nil, fmt.Errorf("failed to build model discovery request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("nvidia endpoint connection failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, nil, fmt.Errorf("nvidia rejected request with status code: %d", resp.StatusCode)
	}

	var modelList NvidiaModelListResponse
	if err := json.NewDecoder(resp.Body).Decode(&modelList); err != nil {
		return 0, nil, fmt.Errorf("failed to parse nvidia models json stream: %w", err)
	}

	// Encrypt the credentials prior to database transaction storage
	encryptedKey, err := vault.Encrypt(apiKey)
	if err != nil {
		return 0, nil, fmt.Errorf("vault encryption failed: %w", err)
	}

	var discoveredModels []string

	// Find which pools already have this apiKey bound to avoid duplicates.
	alreadyBound := make(map[int]bool)
	rows, err := db.Query(ctx, `SELECT pool_id, encrypted_key FROM credentials WHERE provider = $1 AND base_url = $2`, "nvidia", baseURL)
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

	// 2. Open a transaction block to safely write configuration metrics
	tx, err := db.Begin(ctx)
	if err != nil {
		return 0, nil, err
	}
	defer tx.Rollback(ctx)

	for _, m := range modelList.Data {
		// Register each model under two pool patterns:
		//
		//   1. Prefixed form  "nvidia/X" — used by clients that select NVIDIA
		//      explicitly. The handler.go isNvidia block detects this prefix and
		//      strips it from the JSON body before forwarding to NVIDIA NIM.
		//
		//   2. Clean form     "X"        — required for client tools (Cline,
		//      LobeChat, Open WebUI …) that hardcode model name whitelists and
		//      reject any unknown prefix. When the clean form is used the handler
		//      skips the isNvidia body-rewrite, but that is correct: the JSON
		//      body already contains the clean model ID that NVIDIA NIM expects.
		patterns := []string{"nvidia/" + m.ID, m.ID}

		for _, modelPattern := range patterns {
			var poolID int

			// Classify capabilities — NVIDIA models are often reasoning-capable
			caps := ClassifyModel(modelPattern)
			capsJSON, err := json.Marshal(caps.ToMap())
			if err != nil {
				capsJSON = []byte("{}")
			}

			// Insert or fetch the model pool ID, updating capabilities on conflict
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

			// Connect the discovered model pool directly to our hardware key credential
			if !alreadyBound[poolID] {
				_, err = tx.Exec(ctx,
					`INSERT INTO credentials (pool_id, provider, encrypted_key, base_url, weight, is_healthy)
					 VALUES ($1, 'nvidia', $2, $3, $4, true)`,
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

	// 3. Emit PostgreSQL NOTIFY channel statement to instantly trigger SyncManager cache swap
	_, err = tx.Exec(ctx, "NOTIFY config_change, 'model_pools:reload'")
	if err != nil {
		return 0, nil, fmt.Errorf("failed to broadcast config change notification: %w", err)
	}

	return len(discoveredModels), discoveredModels, tx.Commit(ctx)
}
