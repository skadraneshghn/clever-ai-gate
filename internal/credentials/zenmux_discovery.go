package credentials

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ZenMuxBaseURL is the official API base URL for ZenMux.
const ZenMuxBaseURL = "https://zenmux.ai/api/v1"

// DiscoverAndRegisterZenMuxModels connects to ZenMux API, validates the API key,
// discovers all available models from its aggregation list, and registers them
// into the gateway's model pools.
//
// Like other providers, each model is registered under two patterns:
//   1. "zenmux/<provider>/<model_name>" (prefixed routing path)
//   2. "<provider>/<model_name>" (clean path)
func DiscoverAndRegisterZenMuxModels(ctx context.Context, db *pgxpool.Pool, vault *Vault, apiKey string, weight int) (int, []string, error) {
	if apiKey == "" {
		return 0, nil, fmt.Errorf("api_key is required for ZenMux model discovery")
	}

	if weight <= 0 {
		weight = 1
	}

	// 1. Validate the API key and fetch the models list
	dialer := &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return DialContextResilient(ctx, dialer, network, addr)
		},
		TLSHandshakeTimeout: 10 * time.Second,
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   15 * time.Second,
	}
	req, err := http.NewRequestWithContext(ctx, "GET", ZenMuxBaseURL+"/models", nil)
	if err != nil {
		return 0, nil, fmt.Errorf("failed to build ZenMux model discovery request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("ZenMux endpoint connection failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return 0, nil, fmt.Errorf("ZenMux API key validation failed: provider returned status %d", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return 0, nil, fmt.Errorf("ZenMux endpoint rejected request with status code: %d", resp.StatusCode)
	}

	var modelList OpenAIModelListResponse
	if err := json.NewDecoder(resp.Body).Decode(&modelList); err != nil {
		return 0, nil, fmt.Errorf("failed to parse ZenMux models response: %w", err)
	}

	if len(modelList.Data) == 0 {
		return 0, nil, fmt.Errorf("ZenMux returned 0 models")
	}

	// Encrypt the API key
	encryptedKey, err := vault.Encrypt(apiKey)
	if err != nil {
		return 0, nil, fmt.Errorf("vault encryption failed: %w", err)
	}

	var discoveredModels []string

	// Find which pools already have this apiKey bound to avoid duplicates.
	alreadyBound := make(map[int]bool)
	rows, err := db.Query(ctx, `SELECT pool_id, encrypted_key FROM credentials WHERE provider = $1 AND base_url = $2`, "zenmux", ZenMuxBaseURL)
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

	// 2. Open transaction to register pools and credentials
	tx, err := db.Begin(ctx)
	if err != nil {
		return 0, nil, fmt.Errorf("failed to start database transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	for _, m := range modelList.Data {
		if m.ID == "" {
			continue
		}

		// Register under both prefixed alias and clean model identifier
		patterns := []string{"zenmux/" + m.ID, m.ID}

		for _, modelPattern := range patterns {
			var poolID int

			// Classify capabilities (Vision, Reasoning, etc.) from the model pattern
			caps := ClassifyModel(modelPattern)
			capsJSON, err := json.Marshal(caps.ToMap())
			if err != nil {
				capsJSON = []byte("{}")
			}

			// Upsert model pool
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

			// Bind credential to pool
			if !alreadyBound[poolID] {
				_, err = tx.Exec(ctx,
					`INSERT INTO credentials (pool_id, provider, encrypted_key, base_url, weight, is_healthy)
					 VALUES ($1, 'zenmux', $2, $3, $4, true)`,
					poolID, encryptedKey, ZenMuxBaseURL, weight,
				)
				if err != nil {
					return 0, nil, fmt.Errorf("failed to bind ZenMux credential to pool %s: %w", modelPattern, err)
				}
				alreadyBound[poolID] = true
			}

			discoveredModels = append(discoveredModels, modelPattern)
		}
	}

	// 3. Trigger instant routing cache reload
	if _, err = tx.Exec(ctx, "NOTIFY config_change, 'model_pools:reload'"); err != nil {
		return 0, nil, fmt.Errorf("failed to broadcast config change notification: %w", err)
	}

	return len(discoveredModels), discoveredModels, tx.Commit(ctx)
}
