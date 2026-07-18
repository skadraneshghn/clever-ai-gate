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

const (
	geminiBaseURL    = "https://generativelanguage.googleapis.com"
	geminiModelsPath = "/v1beta/models"
)

// geminiModelsResponse is the response from the Google AI Studio /v1beta/models endpoint.
type geminiModelsResponse struct {
	Models        []geminiModelInfo `json:"models"`
	NextPageToken string            `json:"nextPageToken"`
}

// geminiModelInfo holds raw metadata for a single model from the discovery endpoint.
type geminiModelInfo struct {
	// Name is the fully-qualified resource name, e.g. "models/gemini-2.5-pro".
	Name                       string   `json:"name"`
	DisplayName                string   `json:"displayName"`
	Description                string   `json:"description"`
	SupportedGenerationMethods []string `json:"supportedGenerationMethods"`
	InputTokenLimit            int64    `json:"inputTokenLimit"`
	OutputTokenLimit           int64    `json:"outputTokenLimit"`
}

// cleanGeminiModelID strips the "models/" resource prefix that the Google API
// includes in the name field (e.g. "models/gemini-2.5-pro" -> "gemini-2.5-pro").
func cleanGeminiModelID(rawName string) string {
	return strings.TrimPrefix(rawName, "models/")
}

// geminiSupportsGeneration returns true if the model supports text generation
// via generateContent (the method the gateway uses for chat completions).
func geminiSupportsGeneration(methods []string) bool {
	for _, m := range methods {
		if m == "generateContent" {
			return true
		}
	}
	return false
}

// geminiSupportsEmbedding returns true if the model is an embedding model
// (embedContent method present, generateContent absent).
func geminiSupportsEmbedding(methods []string) bool {
	hasEmbed := false
	hasGenerate := false
	for _, m := range methods {
		switch m {
		case "embedContent", "batchEmbedContents":
			hasEmbed = true
		case "generateContent":
			hasGenerate = true
		}
	}
	return hasEmbed && !hasGenerate
}

// fetchGeminiModels retrieves all available models from the Google AI Studio API,
// handling pagination automatically.
func fetchGeminiModels(ctx context.Context, apiKey string) ([]geminiModelInfo, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	var allModels []geminiModelInfo
	pageToken := ""

	for {
		url := geminiBaseURL + geminiModelsPath + "?key=" + apiKey + "&pageSize=100"
		if pageToken != "" {
			url += "&pageToken=" + pageToken
		}

		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to build gemini model discovery request: %w", err)
		}
		req.Header.Set("Accept", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("gemini endpoint connection failed: %w", err)
		}

		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			resp.Body.Close()
			return nil, fmt.Errorf("gemini api key rejected with status %d — verify your AIzaSy... key is valid and has Generative Language API enabled", resp.StatusCode)
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return nil, fmt.Errorf("gemini models endpoint returned status %d", resp.StatusCode)
		}

		var page geminiModelsResponse
		if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("failed to parse gemini models response: %w", err)
		}
		resp.Body.Close()

		allModels = append(allModels, page.Models...)

		if page.NextPageToken == "" {
			break
		}
		pageToken = page.NextPageToken
	}

	return allModels, nil
}

// DiscoverAndRegisterGeminiModels connects to the Google AI Studio REST endpoint,
// fetches all available models, filters them for generateContent or embedContent
// support, upserts model pools, and binds the credential in a single transaction.
//
// Registration strategy (mirrors nvidia_discovery.go dual-pool pattern):
//
//  1. Prefixed form "gemini/<modelID>" — explicitly routes to Gemini. The
//     handler.go isGemini block detects this prefix and activates the full
//     request transpilation pipeline.
//
//  2. Clean form "<modelID>" (e.g. "gemini-2.5-pro") — required for client
//     tools (Cline, Continue, Open WebUI, Kilo Code) that reference models by
//     their canonical names. The credential Provider="gemini" field ensures
//     the transpilation pipeline is still activated even without the prefix.
//
// Returns the count of registered pool patterns, their names, and any error.
func DiscoverAndRegisterGeminiModels(ctx context.Context, db *pgxpool.Pool, vault *Vault, apiKey string, weight int) (int, []string, error) {
	if apiKey == "" {
		return 0, nil, fmt.Errorf("gemini api key is required for model discovery")
	}
	if weight <= 0 {
		weight = 1
	}

	// 1. Fetch live model list from Google AI Studio
	models, err := fetchGeminiModels(ctx, apiKey)
	if err != nil {
		return 0, nil, err
	}

	if len(models) == 0 {
		return 0, nil, fmt.Errorf("gemini returned zero models — check that the Generative Language API is enabled in your Google Cloud project")
	}

	// 2. Encrypt the API key once before the database loop
	encryptedKey, err := vault.Encrypt(apiKey)
	if err != nil {
		return 0, nil, fmt.Errorf("vault encryption failed: %w", err)
	}

	// 3. Find which pools already have THIS exact API key bound (healthy OR unhealthy),
	// tracking the credential row id per pool. A pool that already holds a DIFFERENT key
	// is intentionally NOT marked here, so the new key gets appended (round-robin) instead
	// of overwriting the existing one — this is what allows many keys to coexist per pool.
	thisKeyCredIDByPool := make(map[int]int)
	rows, err := db.Query(ctx,
		`SELECT id, pool_id, encrypted_key FROM credentials WHERE provider = $1 AND base_url = $2`,
		ProviderGemini, geminiBaseURL,
	)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var credID, poolID int
			var encKey string
			if err := rows.Scan(&credID, &poolID, &encKey); err == nil {
				decrypted, decErr := vault.Decrypt(encKey)
				if decErr == nil && decrypted == apiKey {
					thisKeyCredIDByPool[poolID] = credID
				}
			}
		}
	}

	// 4. Open a transaction to atomically write all pools and credentials
	tx, err := db.Begin(ctx)
	if err != nil {
		return 0, nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var discoveredModels []string

	for _, m := range models {
		modelID := cleanGeminiModelID(m.Name)
		if modelID == "" {
			continue
		}

		// Only register models that can be used for generation or embedding.
		// Exclude legacy/deprecated models that only support countTokens etc.
		supportsGen := geminiSupportsGeneration(m.SupportedGenerationMethods)
		supportsEmbed := geminiSupportsEmbedding(m.SupportedGenerationMethods)
		if !supportsGen && !supportsEmbed {
			continue
		}

		// Build capability flags. Start with heuristic classifier,
		// then enrich with live data from the API response.
		caps := ClassifyModel(modelID)
		if supportsEmbed {
			caps.Embedding = true
		}
		// Gemini models with "thinking" or "2.5" in their ID support reasoning
		lower := strings.ToLower(modelID)
		if strings.Contains(lower, "thinking") || strings.Contains(lower, "gemini-2.5") || strings.Contains(lower, "gemini-exp") {
			caps.Reasoning = true
		}
		// All non-embedding Gemini models support vision natively
		if supportsGen {
			caps.Vision = true
		}

		capsJSON, err := json.Marshal(caps.ToMap())
		if err != nil {
			capsJSON = []byte("{}")
		}

		// Register both "gemini/<modelID>" and clean "<modelID>" forms.
		patterns := []string{"gemini/" + modelID, modelID}

		for _, pattern := range patterns {
			var poolID int

			err = tx.QueryRow(ctx,
				`INSERT INTO model_pools (model_pattern, strategy, capabilities)
				 VALUES ($1, 'round-robin', $2)
				 ON CONFLICT (model_pattern) DO UPDATE
				 SET capabilities = EXCLUDED.capabilities
				 RETURNING id`,
				pattern, capsJSON,
			).Scan(&poolID)
			if err != nil {
				return 0, nil, fmt.Errorf("failed to upsert model pool for %s: %w", pattern, err)
			}

			if credID, already := thisKeyCredIDByPool[poolID]; already {
				// This exact key is already bound to this pool. Heal its own row in place so a
				// prior failed health-check probe doesn't permanently block the pool. Scoped to
				// THIS row only — never touch other keys' rows, never insert a duplicate.
				if _, err = tx.Exec(ctx,
					`UPDATE credentials SET is_healthy = true, last_error = NULL WHERE id = $1`,
					credID,
				); err != nil {
					return 0, nil, fmt.Errorf("failed to heal gemini credential for pool %s: %w", pattern, err)
				}
			} else {
			// This key is not yet in this pool — append a new credential so multiple distinct
			// Google AI Studio keys coexist and load-balance together in the same pool.
			_, err = tx.Exec(ctx,
				`INSERT INTO credentials (pool_id, provider, encrypted_key, base_url, weight, is_healthy)
				 VALUES ($1, $2, $3, $4, $5, true)`,
				poolID, ProviderGemini, encryptedKey, geminiBaseURL, weight,
			)
			if err != nil {
				return 0, nil, fmt.Errorf("failed to bind gemini credential to pool %s: %w", pattern, err)
			}
			}

			discoveredModels = append(discoveredModels, pattern)
		}
	}

	// 5. Emit PostgreSQL NOTIFY to instantly trigger SyncManager cache reload
	_, err = tx.Exec(ctx, "NOTIFY config_change, 'model_pools:reload'")
	if err != nil {
		return 0, nil, fmt.Errorf("failed to broadcast config change notification: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, nil, fmt.Errorf("transaction commit failed: %w", err)
	}

	return len(discoveredModels), discoveredModels, nil
}
