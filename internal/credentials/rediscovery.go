package credentials

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

// ReDiscoveryReport holds the aggregated results of a full provider re-discovery scan.
// Serialized to JSON and stored in job_runs.output by the scheduler framework.
type ReDiscoveryReport struct {
	TotalEndpointsScanned int                   `json:"total_endpoints_scanned"`
	SuccessfulEndpoints   int                   `json:"successful_endpoints"`
	FailedEndpoints       int                   `json:"failed_endpoints"`
	NewModelsAdded        int                   `json:"new_models_added"`
	TotalModelsSynced     int                   `json:"total_models_synced"`
	NewModels             []string              `json:"new_models"`
	ProviderBreakdown     []ProviderScanResult  `json:"provider_breakdown"`
	Errors                []string              `json:"errors,omitempty"`
	StartedAt             time.Time             `json:"started_at"`
	CompletedAt           time.Time             `json:"completed_at"`
	DurationMs            int64                 `json:"duration_ms"`
}

// ProviderScanResult records the outcome of scanning a single provider account.
type ProviderScanResult struct {
	Provider     string   `json:"provider"`
	BaseURL      string   `json:"base_url"`
	Status       string   `json:"status"` // "success" or "failed"
	ModelsSynced int      `json:"models_synced"`
	NewModels    []string `json:"new_models"`
	Error        string   `json:"error,omitempty"`
}

// providerAccount mirrors the DISTINCT query result used for re-discovery scanning.
type providerAccount struct {
	Provider     string
	EncryptedKey string
	BaseURL      string
	Prefix       string
	Weight       int
}

// RunReDiscovery performs a full re-discovery scan across all registered provider accounts.
//
// The algorithm:
//  1. Snapshot every existing model_pattern from model_pools
//  2. Query all distinct (provider, encrypted_key, base_url, prefix) tuples from credentials
//  3. For each account, call the appropriate DiscoverAndRegister* function
//  4. Diff the post-scan model_pools against the snapshot to find truly new models
//  5. Build and return a ReDiscoveryReport
//
// This reuses the exact same per-provider discovery functions that RefreshAllProviders
// calls synchronously — the difference is that this function runs inside an async job
// and produces a structured diff-based report.
func RunReDiscovery(ctx context.Context, db *pgxpool.Pool, vault *Vault, logger *zap.Logger) (*ReDiscoveryReport, error) {
	startTime := time.Now()

	report := &ReDiscoveryReport{
		NewModels:         make([]string, 0),
		ProviderBreakdown: make([]ProviderScanResult, 0),
		Errors:            make([]string, 0),
		StartedAt:         startTime,
	}

	// 1. Snapshot all existing model_patterns before discovery
	existingPatterns, err := snapshotModelPatterns(ctx, db)
	if err != nil {
		return nil, fmt.Errorf("failed to snapshot existing model patterns: %w", err)
	}
	logger.Info("re-discovery: existing model patterns snapshot taken",
		zap.Int("existing_count", len(existingPatterns)),
	)

	// 2. Query all distinct provider accounts
	accounts, err := queryDistinctAccounts(ctx, db)
	if err != nil {
		return nil, fmt.Errorf("failed to query provider accounts: %w", err)
	}
	report.TotalEndpointsScanned = len(accounts)

	if len(accounts) == 0 {
		report.CompletedAt = time.Now()
		report.DurationMs = time.Since(startTime).Milliseconds()
		return report, nil
	}

	logger.Info("re-discovery: scanning provider accounts",
		zap.Int("account_count", len(accounts)),
	)

	// 3. Re-run discovery for each unique account
	for _, acc := range accounts {
		apiKey, decErr := vault.Decrypt(acc.EncryptedKey)
		if decErr != nil {
			errMsg := fmt.Sprintf("%s(%s): decrypt error: %v", acc.Provider, acc.BaseURL, decErr)
			report.Errors = append(report.Errors, errMsg)
			report.FailedEndpoints++
			report.ProviderBreakdown = append(report.ProviderBreakdown, ProviderScanResult{
				Provider: acc.Provider,
				BaseURL:  acc.BaseURL,
				Status:   "failed",
				Error:    errMsg,
			})
			continue
		}

		weight := acc.Weight
		if weight <= 0 {
			weight = 1
		}

		count, discovered, discErr := callProviderDiscovery(ctx, db, vault, acc, apiKey, weight)
		if discErr != nil {
			errMsg := fmt.Sprintf("%s(%s): %v", acc.Provider, acc.BaseURL, discErr)
			report.Errors = append(report.Errors, errMsg)
			report.FailedEndpoints++
			report.ProviderBreakdown = append(report.ProviderBreakdown, ProviderScanResult{
				Provider: acc.Provider,
				BaseURL:  acc.BaseURL,
				Status:   "failed",
				Error:    errMsg,
			})
			logger.Warn("re-discovery: provider scan failed",
				zap.String("provider", acc.Provider),
				zap.String("base_url", acc.BaseURL),
				zap.Error(discErr),
			)
			continue
		}

		// Identify which discovered models are actually new
		var newFromThisProvider []string
		for _, modelPattern := range discovered {
			if !existingPatterns[modelPattern] {
				newFromThisProvider = append(newFromThisProvider, modelPattern)
			}
		}

		report.SuccessfulEndpoints++
		report.TotalModelsSynced += count
		report.ProviderBreakdown = append(report.ProviderBreakdown, ProviderScanResult{
			Provider:     acc.Provider,
			BaseURL:      acc.BaseURL,
			Status:       "success",
			ModelsSynced: count,
			NewModels:    newFromThisProvider,
		})

		logger.Info("re-discovery: provider scan completed",
			zap.String("provider", acc.Provider),
			zap.String("base_url", acc.BaseURL),
			zap.Int("synced", count),
			zap.Int("new", len(newFromThisProvider)),
		)
	}

	// 4. Post-scan diff: query model_pools again and find truly new patterns
	postPatterns, err := snapshotModelPatterns(ctx, db)
	if err != nil {
		logger.Warn("re-discovery: failed to snapshot post-scan patterns", zap.Error(err))
		// Non-fatal: fall back to per-provider new model tracking
	} else {
		for pattern := range postPatterns {
			if !existingPatterns[pattern] {
				report.NewModels = append(report.NewModels, pattern)
			}
		}
	}
	report.NewModelsAdded = len(report.NewModels)

	// 5. Finalize timing
	report.CompletedAt = time.Now()
	report.DurationMs = time.Since(startTime).Milliseconds()

	logger.Info("re-discovery: scan complete",
		zap.Int("endpoints_scanned", report.TotalEndpointsScanned),
		zap.Int("successful", report.SuccessfulEndpoints),
		zap.Int("failed", report.FailedEndpoints),
		zap.Int("new_models", report.NewModelsAdded),
		zap.Int("total_synced", report.TotalModelsSynced),
		zap.Int64("duration_ms", report.DurationMs),
	)

	return report, nil
}

// snapshotModelPatterns returns a set of all existing model_pattern values from model_pools.
func snapshotModelPatterns(ctx context.Context, db *pgxpool.Pool) (map[string]bool, error) {
	rows, err := db.Query(ctx, `SELECT model_pattern FROM model_pools`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	patterns := make(map[string]bool)
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		patterns[p] = true
	}
	return patterns, nil
}

// queryDistinctAccounts returns all unique provider accounts from the credentials table.
func queryDistinctAccounts(ctx context.Context, db *pgxpool.Pool) ([]providerAccount, error) {
	rows, err := db.Query(ctx, `
		SELECT DISTINCT ON (provider, encrypted_key, base_url, COALESCE(prefix,''))
		       provider, encrypted_key, base_url, COALESCE(prefix,'') AS prefix,
		       MAX(weight) OVER (PARTITION BY provider, encrypted_key, base_url, COALESCE(prefix,'')) AS weight
		FROM credentials
		ORDER BY provider, encrypted_key, base_url, COALESCE(prefix,'')
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var accounts []providerAccount
	for rows.Next() {
		var acc providerAccount
		if err := rows.Scan(&acc.Provider, &acc.EncryptedKey, &acc.BaseURL, &acc.Prefix, &acc.Weight); err != nil {
			continue
		}
		accounts = append(accounts, acc)
	}
	return accounts, nil
}

// callProviderDiscovery dispatches to the correct DiscoverAndRegister* function based on provider type.
func callProviderDiscovery(ctx context.Context, db *pgxpool.Pool, vault *Vault, acc providerAccount, apiKey string, weight int) (int, []string, error) {
	switch acc.Provider {
	case "nvidia":
		return DiscoverAndRegisterNvidiaModels(ctx, db, vault, apiKey, acc.BaseURL, weight)

	case "ollama":
		return DiscoverAndRegisterOllamaModels(ctx, db, vault, apiKey, acc.BaseURL, weight)

	case "openrouter":
		return DiscoverAndRegisterOpenRouterModels(ctx, db, vault, apiKey, weight)

	case "1minai":
		return DiscoverAndRegisterOneMinAIModels(ctx, db, vault, apiKey, weight)

	case "cloudflare":
		// Recover the account ID from the stored base_url convention.
		// base_url is stored as "cloudflare:<accountID>" by RegisterCloudflareProvider.
		accountID := strings.TrimPrefix(acc.BaseURL, "cloudflare:")
		return DiscoverAndRegisterCloudflareModels(ctx, db, vault, accountID, apiKey, weight)

	case "sarvam":
		return DiscoverAndRegisterSarvamModels(ctx, db, vault, apiKey, weight)

	case "puter":
		return DiscoverAndRegisterPuterModels(ctx, db, vault, apiKey, weight)

	default:
		// Any OpenAI-compatible provider (openai, anthropic, deepseek, custom, …)
		return DiscoverAndRegisterCustomModels(ctx, db, vault, apiKey, acc.BaseURL, acc.Provider, weight, acc.Prefix)
	}
}

// MarshalReport serializes a ReDiscoveryReport to a JSON string suitable for job_runs.output storage.
func MarshalReport(report *ReDiscoveryReport) string {
	data, err := json.Marshal(report)
	if err != nil {
		return fmt.Sprintf(`{"error":"failed to marshal report: %s"}`, err.Error())
	}
	return string(data)
}
