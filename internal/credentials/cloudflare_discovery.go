package credentials

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cloudflare/cloudflare-go/v7"
	"github.com/cloudflare/cloudflare-go/v7/ai"
	"github.com/cloudflare/cloudflare-go/v7/option"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

const cloudflareAPIBase = "https://api.cloudflare.com/client/v4"

// cloudflareModelSearchResponse maps the Cloudflare
// GET /accounts/{id}/ai/models/search JSON envelope.
type cloudflareModelSearchResponse struct {
	Result  []cloudflareModel `json:"result"`
	Success bool              `json:"success"`
	Errors  []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

// cloudflareModel represents a single entry from the Cloudflare AI model catalog.
type cloudflareModel struct {
	// ID is the canonical model identifier, e.g. "@cf/meta/llama-3.1-8b-instruct".
	ID   string `json:"id"`
	Name string `json:"name"`

	// Task holds the modality classification provided by Cloudflare.
	// Using the task.name field directly avoids heuristic guessing.
	Task struct {
		Name string `json:"name"`
	} `json:"task"`
}

// cloudflareTaskCapabilities maps Cloudflare task names to ModelCapabilities.
// Unlike other providers where ClassifyModel guesses from the model ID,
// Cloudflare provides ground-truth task classification — so we use it directly.
func cloudflareTaskCapabilities(taskName string) ModelCapabilities {
	lower := strings.ToLower(taskName)
	caps := ModelCapabilities{}

	switch {
	case strings.Contains(lower, "text-to-image") ||
		strings.Contains(lower, "image generation"):
		caps.ImageGeneration = true

	case strings.Contains(lower, "text embedding") ||
		strings.Contains(lower, "embedding"):
		caps.Embedding = true

	case strings.Contains(lower, "speech recognition") ||
		strings.Contains(lower, "transcription") ||
		strings.Contains(lower, "text-to-speech") ||
		strings.Contains(lower, "speech synthesis"):
		caps.Audio = true

	case strings.Contains(lower, "image-to-text") ||
		strings.Contains(lower, "visual question"):
		caps.Vision = true

	// "Text Generation", "Summarization", etc. → plain chat model, no special flags
	}

	return caps
}

// DiscoverAndRegisterCloudflareModels connects to Cloudflare Workers AI,
// fetches the full model catalog for the given account, auto-provisions
// model pools in PostgreSQL, and binds the API token credential to all of
// them in a single atomic transaction.
//
// Credential storage convention (no schema migration required):
//   - encrypted_key: the API Token, encrypted via AES-256-GCM vault.
//   - base_url:      "cloudflare:<accountID>" — the rewriter parses the
//     account ID from this field using strings.TrimPrefix.
//
// Each model is registered under two pool patterns:
//
//  1. "cloudflare/@cf/meta/llama-3.1-8b-instruct" — explicit prefix form.
//     Handler detects "cloudflare/" prefix and strips it from the JSON body.
//
//  2. "@cf/meta/llama-3.1-8b-instruct" — clean form for strict client tools
//     (Cline, LobeChat, Open WebUI) that reject unknown prefixes.
//
// If the same account ID + API token is re-submitted, existing pool bindings
// are detected and skipped to avoid duplicate credential rows.
func DiscoverAndRegisterCloudflareModels(
	ctx context.Context,
	db *pgxpool.Pool,
	vault *Vault,
	accountID, apiToken string,
	weight int,
) (int, []string, error) {
	accountID = strings.TrimSpace(accountID)
	apiToken = strings.TrimSpace(apiToken)
	if strings.HasPrefix(apiToken, "Bearer ") {
		apiToken = strings.TrimPrefix(apiToken, "Bearer ")
		apiToken = strings.TrimSpace(apiToken)
	}

	if accountID == "" {
		return 0, nil, fmt.Errorf("cloudflare account_id is required")
	}
	if apiToken == "" {
		return 0, nil, fmt.Errorf("cloudflare api_token is required")
	}

	// 1. Fetch the live model catalog from Cloudflare.
	models, err := fetchCloudflareModels(ctx, accountID, apiToken)
	if err != nil {
		return 0, nil, err
	}

	// 2. Encrypt the API token before any DB write.
	encryptedToken, err := vault.Encrypt(apiToken)
	if err != nil {
		return 0, nil, fmt.Errorf("vault encryption failed: %w", err)
	}

	// The base_url column stores the account ID using the "cloudflare:" prefix
	// convention so that RefreshAllProviders and the rewriter can recover it
	// without a separate schema column.
	storedBaseURL := "cloudflare:" + accountID

	// 3. Find which pools already have this token bound (idempotent re-discovery).
	alreadyBound := make(map[int]bool)
	rows, err := db.Query(ctx,
		`SELECT pool_id, encrypted_key FROM credentials WHERE provider = $1 AND base_url = $2`,
		"cloudflare", storedBaseURL,
	)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var poolID int
			var encKey string
			if err := rows.Scan(&poolID, &encKey); err == nil {
				decrypted, decErr := vault.Decrypt(encKey)
				if decErr == nil && decrypted == apiToken {
					alreadyBound[poolID] = true
				}
			}
		}
		rows.Close()
	}

	// 4. Open a transaction to atomically write all pools and credentials.
	tx, err := db.Begin(ctx)
	if err != nil {
		return 0, nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck — intentional deferred cleanup

	var discoveredModels []string

	for _, m := range models {
		if m.Name == "" {
			continue
		}

		// Build capabilities from the ground-truth Cloudflare task name.
		caps := cloudflareTaskCapabilities(m.Task.Name)
		capsJSON, err := json.Marshal(caps.ToMap())
		if err != nil {
			capsJSON = []byte("{}")
		}

		// Register under two pool patterns for maximum client compatibility.
		patterns := []string{"cloudflare/" + m.Name, m.Name}

		for _, modelPattern := range patterns {
			var poolID int

			// Upsert the model pool; ON CONFLICT updates capabilities on re-discovery.
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

			// Bind the Cloudflare credential to this pool (idempotent).
			if !alreadyBound[poolID] {
				_, err = tx.Exec(ctx,
					`INSERT INTO credentials (pool_id, provider, encrypted_key, base_url, weight, is_healthy)
					 VALUES ($1, 'cloudflare', $2, $3, $4, true)`,
					poolID, encryptedToken, storedBaseURL, weight,
				)
				if err != nil {
					return 0, nil, fmt.Errorf("failed to bind credential to pool %s: %w", modelPattern, err)
				}
				alreadyBound[poolID] = true
			}

			discoveredModels = append(discoveredModels, modelPattern)
		}
	}

	// 5. Notify the SyncManager to instantly hot-reload the routing cache.
	if _, err = tx.Exec(ctx, "NOTIFY config_change, 'model_pools:reload'"); err != nil {
		return 0, nil, fmt.Errorf("failed to broadcast config change notification: %w", err)
	}

	return len(discoveredModels), discoveredModels, tx.Commit(ctx)
}

// fetchCloudflareModels calls the Cloudflare Workers AI model catalog endpoint
// and returns the FULL model list for the given account by auto-paginating
// through every page of results.
//
// The SDK's ListAutoPaging iterator handles page cursors automatically,
// ensuring no models are missed regardless of catalog size.
//
// Auth: Authorization: Bearer {apiToken}
//
// If Cloudflare returns 401/403, an explicit auth error is returned so
// the admin panel can surface a clear rejection message.
func fetchCloudflareModels(ctx context.Context, accountID, apiToken string) ([]cloudflareModel, error) {
	// Initialize client using the official cloudflare-go v7 SDK
	client := cloudflare.NewClient(option.WithAPIToken(apiToken))

	// Use ListAutoPaging to iterate through ALL pages of the model catalog.
	// The default List() only returns the first page (~20-50 models),
	// which is why many models (kimi-k3, gpt-5.6-*, claude-sonnet-5, etc.)
	// were previously missing.
	iter := client.AI.Models.ListAutoPaging(ctx, ai.ModelListParams{
		AccountID: cloudflare.F(accountID),
		PerPage:   cloudflare.F(int64(200)), // maximize items per page to reduce round-trips
	})

	var models []cloudflareModel

	for iter.Next() {
		item := iter.Current()

		// Each item is interface{}, so marshal → unmarshal to our struct.
		itemJSON, err := json.Marshal(item)
		if err != nil {
			continue
		}

		var m cloudflareModel
		if err := json.Unmarshal(itemJSON, &m); err != nil {
			continue
		}

		models = append(models, m)
	}

	if err := iter.Err(); err != nil {
		tokenSnippet := ""
		if len(apiToken) >= 6 {
			tokenSnippet = apiToken[:6]
		} else {
			tokenSnippet = apiToken
		}
		if prodLogger, logErr := zap.NewProduction(); logErr == nil {
			prodLogger.Error("Cloudflare auto-discovery request failed via Go SDK v7",
				zap.String("token_prefix", tokenSnippet),
				zap.Error(err),
			)
			_ = prodLogger.Sync()
		}
		return nil, fmt.Errorf("cloudflare Workers AI auto-discovery failed: %w", err)
	}

	if len(models) == 0 {
		return nil, fmt.Errorf(
			"cloudflare returned an empty model catalog — " +
				"verify your account has Workers AI access enabled",
		)
	}

	return models, nil
}

