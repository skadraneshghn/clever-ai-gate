package credentials

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/cloudflare/cloudflare-go/v7"
	"github.com/cloudflare/cloudflare-go/v7/ai"
	"github.com/cloudflare/cloudflare-go/v7/option"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

const cloudflareAPIBase = "https://api.cloudflare.com/client/v4"

// cloudflareDocsManifestURL is the public Cloudflare documentation index that
// lists every model available on the AI Gateway, including third-party proxied
// models that are never returned by the authenticated SDK endpoint.
const cloudflareDocsManifestURL = "https://developers.cloudflare.com/ai/models/index.md"

// cfDocsModelRegex matches model link paths embedded in the Cloudflare docs
// markdown index.  Link targets appear as:
//
//	https://developers.cloudflare.com/ai/models/@cf/meta/llama-3.1-8b-instruct/
//	https://developers.cloudflare.com/ai/models/openai/gpt-5/
//
// Capture group 1 holds the raw model path (e.g. "@cf/meta/llama-3.1-8b-instruct"
// or "openai/gpt-5").
var cfDocsModelRegex = regexp.MustCompile(`/ai/models/((?:@[a-zA-Z0-9_-]+/[a-zA-Z0-9_.\-]+/[a-zA-Z0-9_.\-]+|[a-zA-Z0-9_.\-]+/[a-zA-Z0-9_.\-]+))/`)

// cfDocsTaskRegex extracts a task/modality keyword from the surrounding text
// in the docs manifest so we can infer model capabilities without an API call.
// We look for well-known task labels that appear close to each model link.
var cfDocsTaskRegex = regexp.MustCompile(`(?i)(Text Generation|Text-to-Image|Image-to-Text|Text-to-Speech|Automatic Speech Recognition|Text Embedding|Text-to-Video|Image-to-Video|Music Generation|websocket)`)

// ─── SDK types ───────────────────────────────────────────────────────────────

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

// ─── Capability classifiers ───────────────────────────────────────────────────

// cloudflareTaskCapabilities maps Cloudflare SDK task names to ModelCapabilities.
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

// inferCapabilitiesFromTask maps a docs-manifest task label to ModelCapabilities.
// Used for third-party models where the SDK doesn't provide task metadata.
func inferCapabilitiesFromTask(taskLabel string) ModelCapabilities {
	lower := strings.ToLower(taskLabel)
	caps := ModelCapabilities{}

	switch {
	case strings.Contains(lower, "text-to-image") ||
		strings.Contains(lower, "image generation"):
		caps.ImageGeneration = true

	case strings.Contains(lower, "image-to-text"):
		caps.Vision = true

	case strings.Contains(lower, "text-to-speech") ||
		strings.Contains(lower, "speech") ||
		strings.Contains(lower, "transcription"):
		caps.Audio = true

	case strings.Contains(lower, "embedding"):
		caps.Embedding = true

	// text-to-video, image-to-video, music, websocket → no OpenAI equivalent flags
	}

	return caps
}

// ─── Docs manifest discovery ─────────────────────────────────────────────────

// docModel holds a model discovered from the Cloudflare documentation manifest.
type docModel struct {
	ID       string // e.g. "@cf/meta/llama-3.1-8b-instruct" or "openai/gpt-5"
	TaskHint string // nearby task label from the docs page
}

// fetchCloudflareDocModels fetches the Cloudflare public documentation index and
// parses out every model identifier listed, including third-party proxied models
// that the authenticated SDK endpoint never returns.
//
// Returns a slice of docModel values deduplicated by model ID.
func fetchCloudflareDocModels(ctx context.Context) ([]docModel, error) {
	client := &http.Client{Timeout: 20 * time.Second}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cloudflareDocsManifestURL, nil)
	if err != nil {
		return nil, fmt.Errorf("cloudflare docs manifest: failed to build request: %w", err)
	}
	req.Header.Set("Accept", "text/markdown, text/plain, */*")
	req.Header.Set("User-Agent", "CleverAIGate/1.0 (model-discovery)")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cloudflare docs manifest: HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("cloudflare docs manifest: unexpected status %d", resp.StatusCode)
	}

	// Limit response body to 2MB — the docs page is currently ~80KB.
	// This prevents runaway memory if Cloudflare's page grows significantly.
	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("cloudflare docs manifest: failed to read body: %w", err)
	}

	content := string(bodyBytes)

	// Extract all model path matches
	modelMatches := cfDocsModelRegex.FindAllStringSubmatchIndex(content, -1)

	seen := make(map[string]bool)
	var models []docModel

	for _, matchIdx := range modelMatches {
		if len(matchIdx) < 4 {
			continue
		}
		rawPath := content[matchIdx[2]:matchIdx[3]]

		// Sanitize: trim trailing slashes, clean whitespace
		rawPath = strings.Trim(rawPath, "/ \t\n\r")
		if rawPath == "" || len(rawPath) < 5 {
			continue
		}

		// Skip noise entries that regex could accidentally capture
		if isNoisyPath(rawPath) {
			continue
		}

		if seen[rawPath] {
			continue
		}
		seen[rawPath] = true

		// Try to find a task label near this match to infer capabilities
		taskHint := ""
		scanStart := matchIdx[0]
		if scanStart < 300 {
			scanStart = 0
		} else {
			scanStart -= 300
		}
		scanEnd := matchIdx[1]
		if scanEnd+300 < len(content) {
			scanEnd += 300
		} else {
			scanEnd = len(content)
		}
		neighborhood := content[scanStart:scanEnd]
		if tm := cfDocsTaskRegex.FindString(neighborhood); tm != "" {
			taskHint = tm
		}

		models = append(models, docModel{
			ID:       rawPath,
			TaskHint: taskHint,
		})
	}

	return models, nil
}

// isNoisyPath returns true for path segments that look like model IDs but are
// actually navigation/structural links that the regex could accidentally match.
func isNoisyPath(path string) bool {
	noise := []string{
		"third-party", "cloudflare-hosted", "zero-data", "function-calling",
		"reasoning", "vision", "batch", "skip-to", "content", "models/index",
		"newest-first", "task-types", "capabilities", "providers", "authors",
	}
	lower := strings.ToLower(path)
	for _, n := range noise {
		if strings.Contains(lower, n) {
			return true
		}
	}
	// Must contain exactly one slash (provider/model) or two slashes (@cf/provider/model)
	parts := strings.Split(path, "/")
	if strings.HasPrefix(path, "@cf/") {
		return len(parts) != 3 // @cf/provider/model
	}
	return len(parts) != 2 // provider/model
}

// ─── Main discovery entry point ───────────────────────────────────────────────

// DiscoverAndRegisterCloudflareModels connects to Cloudflare Workers AI using a
// hybrid two-tier discovery strategy:
//
//	Tier 1 — Cloudflare SDK:     native @cf/* Workers AI models for the account
//	Tier 2 — Docs manifest:      ALL models listed on developers.cloudflare.com
//	                              including third-party proxied models (openai/gpt-5,
//	                              anthropic/claude-sonnet-5, google/gemini-2.5-pro…)
//
// Both sets are merged, deduplicated, and registered into model_pools with the
// correct capabilities. Every model is registered under two pool patterns for
// maximum client compatibility:
//
//  1. "cloudflare/openai/gpt-5"  — explicit Cloudflare-prefix routing form.
//  2. "openai/gpt-5"             — clean form for tools that reject unknown prefixes.
//
// Credential storage convention:
//   - encrypted_key: API Token, encrypted via AES-256-GCM vault.
//   - base_url:      "cloudflare:<accountID>" — the rewriter recovers the account
//     ID via strings.TrimPrefix.
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

	// ── Tier 1: SDK — native @cf/* Workers AI models ──────────────────────────
	sdkModels, sdkErr := fetchCloudflareSDKModels(ctx, accountID, apiToken)
	if sdkErr != nil {
		// Non-fatal: log and continue to Tier 2.
		// The account may not have Workers AI enabled but can still use the
		// AI Gateway for third-party models.
		if prodLogger, logErr := zap.NewProduction(); logErr == nil {
			prodLogger.Warn("cloudflare SDK discovery failed (Tier 1), continuing with docs manifest",
				zap.Error(sdkErr),
			)
			_ = prodLogger.Sync()
		}
	}

	// ── Tier 2: Docs manifest — ALL public AI Gateway models ──────────────────
	docModels, docErr := fetchCloudflareDocModels(ctx)
	if docErr != nil {
		if prodLogger, logErr := zap.NewProduction(); logErr == nil {
			prodLogger.Warn("cloudflare docs manifest discovery failed (Tier 2)",
				zap.Error(docErr),
			)
			_ = prodLogger.Sync()
		}
	}

	// If both tiers failed, surface an error.
	if sdkErr != nil && docErr != nil {
		return 0, nil, fmt.Errorf(
			"cloudflare discovery: both tiers failed — SDK: %v; docs: %v",
			sdkErr, docErr,
		)
	}

	// ── Merge: build a unified model map keyed by model ID ────────────────────
	type mergedModel struct {
		ID       string
		TaskName string // from SDK (authoritative) or inferred from docs
	}

	unified := make(map[string]mergedModel)

	// Add SDK models first (task name is authoritative)
	for _, m := range sdkModels {
		if m.Name == "" {
			continue
		}
		unified[m.Name] = mergedModel{ID: m.Name, TaskName: m.Task.Name}
	}

	// Add docs-manifest models, skipping those already added by SDK
	for _, dm := range docModels {
		if _, exists := unified[dm.ID]; !exists {
			unified[dm.ID] = mergedModel{ID: dm.ID, TaskName: dm.TaskHint}
		}
	}

	if len(unified) == 0 {
		return 0, nil, fmt.Errorf(
			"cloudflare returned an empty model catalog — " +
				"verify your account has Workers AI or AI Gateway access enabled",
		)
	}

	// ── Encrypt the API token once before any DB write ────────────────────────
	encryptedToken, err := vault.Encrypt(apiToken)
	if err != nil {
		return 0, nil, fmt.Errorf("vault encryption failed: %w", err)
	}

	storedBaseURL := "cloudflare:" + accountID

	// ── Identify pools already bound to this token (idempotent re-discovery) ──
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

	// ── Transaction: atomically write all pools and credentials ───────────────
	tx, err := db.Begin(ctx)
	if err != nil {
		return 0, nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck — intentional deferred cleanup

	var discoveredModels []string

	for _, m := range unified {
		// Build capabilities from task name (SDK ground-truth) or docs hint
		var caps ModelCapabilities
		if m.TaskName != "" {
			// SDK models have authoritative Cloudflare task names
			// Docs models have task labels like "Text Generation", "Text-to-Image"
			caps = cloudflareTaskCapabilities(m.TaskName)
			if caps == (ModelCapabilities{}) {
				caps = inferCapabilitiesFromTask(m.TaskName)
			}
		}

		capsJSON, err := json.Marshal(caps.ToMap())
		if err != nil {
			capsJSON = []byte("{}")
		}

		// Register under two pool patterns for maximum client compatibility:
		//   1. "cloudflare/@cf/meta/llama-3.1-8b-instruct"  (explicit Cloudflare prefix)
		//   2. "@cf/meta/llama-3.1-8b-instruct"             (clean form)
		patterns := []string{"cloudflare/" + m.ID, m.ID}

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

			// Bind the Cloudflare credential to this pool (idempotent via alreadyBound guard).
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

	// Notify the SyncManager to instantly hot-reload the routing cache.
	if _, err = tx.Exec(ctx, "NOTIFY config_change, 'model_pools:reload'"); err != nil {
		return 0, nil, fmt.Errorf("failed to broadcast config change notification: %w", err)
	}

	return len(discoveredModels), discoveredModels, tx.Commit(ctx)
}

// ─── SDK fetch helper ─────────────────────────────────────────────────────────

// fetchCloudflareSDKModels calls the Cloudflare Workers AI model catalog endpoint
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
func fetchCloudflareSDKModels(ctx context.Context, accountID, apiToken string) ([]cloudflareModel, error) {
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
			prodLogger.Error("Cloudflare SDK discovery (Tier 1) failed",
				zap.String("token_prefix", tokenSnippet),
				zap.Error(err),
			)
			_ = prodLogger.Sync()
		}
		return nil, fmt.Errorf("cloudflare Workers AI SDK discovery failed: %w", err)
	}

	return models, nil
}
