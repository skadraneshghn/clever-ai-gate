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

const agentRouterBaseURL = "https://agentrouter.org/v1"

// agentRouterModelListResponse maps the standard OpenAI GET /v1/models JSON envelope
// returned by AgentRouter's model catalog endpoint.
type agentRouterModelListResponse struct {
	Data []agentRouterModel `json:"data"`
}

// agentRouterModel represents a single model entry from the AgentRouter catalog.
type agentRouterModel struct {
	// ID is the canonical model identifier, e.g. "gpt-4o", "claude-sonnet-4-20250514".
	ID string `json:"id"`
}

// DiscoverAndRegisterAgentRouterModels fetches the full AgentRouter model catalog,
// auto-provisions model_pools in PostgreSQL for each one, and binds the provided API
// key credential to all of them in a single atomic transaction.
//
// This mirrors the pattern established by DiscoverAndRegisterOpenRouterModels and
// DiscoverAndRegisterCloudflareModels for architectural consistency.
//
// Each model is registered under two pool patterns for maximum client compatibility:
//
//  1. "agentrouter/<model-id>" — explicit prefix form. Handler detects "agentrouter/"
//     prefix and strips it from the JSON body before forwarding.
//
//  2. "<model-id>" — clean form for strict client tools (Kilo, Cline, LobeChat, Open
//     WebUI) that reject unknown prefixes. The clean alias is ONLY created if no
//     existing pool already maps to that bare model ID — this prevents AgentRouter
//     pools from shadowing direct OpenAI/Anthropic/DeepSeek keys the user has
//     configured for the same model.
//
// When the same API key is re-submitted, existing pool bindings are detected and
// skipped to avoid duplicate credential rows.
//
// Returns the count of registered model patterns, their IDs, and any error encountered.
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

	// 1. Fetch the live model catalog from AgentRouter.
	models, err := fetchAgentRouterModels(ctx, apiKey)
	if err != nil {
		return 0, nil, err
	}

	// 2. Encrypt the API key once before the database loop (save CPU on hot path).
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

	// 5. Open a transaction to atomically write all pools and credentials.
	tx, err := db.Begin(ctx)
	if err != nil {
		return 0, nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck — intentional deferred cleanup

	var discoveredModels []string

	for _, m := range models {
		if m.ID == "" {
			continue
		}

		// Classify capabilities from the model identifier.
		caps := ClassifyModel(m.ID)
		capsJSON, err := json.Marshal(caps.ToMap())
		if err != nil {
			capsJSON = []byte("{}")
		}

		// Register under two pool patterns for maximum client compatibility.
		//
		//   1. Prefixed form: always created for explicit agentrouter routing.
		//   2. Clean alias:   only if no existing pool already claims that bare model ID.
		//      This prevents AgentRouter from shadowing direct provider keys the user
		//      has configured (e.g., a direct OpenAI key for "gpt-4o").
		type patternEntry struct {
			pattern string
		}

		var patterns []patternEntry
		patterns = append(patterns, patternEntry{pattern: "agentrouter/" + m.ID})
		if !existingPatterns[m.ID] {
			patterns = append(patterns, patternEntry{pattern: m.ID})
		}

		for _, pe := range patterns {
			var poolID int

			// Upsert the model_pool — updates capabilities on re-discovery.
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

			// Bind the AgentRouter credential to this pool (idempotent).
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
			// Track newly created patterns to avoid duplicate clean alias creation
			// within the same transaction.
			existingPatterns[pe.pattern] = true
		}
	}

	// 6. Notify the SyncManager to instantly hot-reload the routing cache.
	if _, err = tx.Exec(ctx, "NOTIFY config_change, 'model_pools:reload'"); err != nil {
		return 0, nil, fmt.Errorf("failed to broadcast config change notification: %w", err)
	}

	return len(discoveredModels), discoveredModels, tx.Commit(ctx)
}

// fetchAgentRouterModels calls GET /v1/models on the AgentRouter API and returns
// the full model catalog. Authentication uses a Bearer token.
//
// AgentRouter exposes a standard OpenAI-compatible /v1/models endpoint that returns
// the same JSON structure as OpenAI: {"data": [{"id": "model-name", ...}]}.
func fetchAgentRouterModels(ctx context.Context, apiKey string) ([]agentRouterModel, error) {
	client := &http.Client{Timeout: 20 * time.Second}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, agentRouterBaseURL+"/models", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build agentrouter discovery request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "CleverAIGate/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("agentrouter api connection failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf(
			"agentrouter rejected the api key (status %d) — "+
				"verify your key at agentrouter.org and ensure this client is allowed",
			resp.StatusCode,
		)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("agentrouter models endpoint returned unexpected status: %d", resp.StatusCode)
	}

	var catalog agentRouterModelListResponse
	if err := json.NewDecoder(resp.Body).Decode(&catalog); err != nil {
		return nil, fmt.Errorf("failed to parse agentrouter model catalog: %w", err)
	}

	if len(catalog.Data) == 0 {
		return nil, fmt.Errorf(
			"agentrouter returned an empty model catalog — " +
				"check your api key permissions and account status",
		)
	}

	return catalog.Data, nil
}
