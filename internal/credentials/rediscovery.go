package credentials

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
	"golang.org/x/sync/semaphore"
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
	WorkerCount           int                   `json:"worker_count"`
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
//  3. Dispatch all accounts concurrently via a bounded worker pool (NumCPU * 4 goroutines)
//  4. Each goroutine runs with an isolated per-provider timeout (default 15s) so one
//     dead endpoint can never block others
//  5. Diff the post-scan model_pools against the snapshot to find truly new models
//  6. Build and return a ReDiscoveryReport
//
// This reuses the exact same per-provider discovery functions that RefreshAllProviders
// calls synchronously — the difference is that this function runs inside an async job,
// executes all providers in parallel across all CPU cores, and produces a structured
// diff-based report.
//
// perProviderTimeoutSec controls the maximum duration for each individual provider
// scan. Pass 0 to use the default of 15 seconds.
func RunReDiscovery(ctx context.Context, db *pgxpool.Pool, vault *Vault, logger *zap.Logger, perProviderTimeoutSec int) (*ReDiscoveryReport, error) {
	startTime := time.Now()

	// Default per-provider timeout: 15 seconds (generous enough for Cloudflare SDK
	// pagination, Gemini page tokens, and OpenRouter's large catalog).
	if perProviderTimeoutSec <= 0 {
		perProviderTimeoutSec = 15
	}
	perProviderTimeout := time.Duration(perProviderTimeoutSec) * time.Second

	// Size the worker pool to saturate all CPU cores for I/O-bound work.
	// Floor of 8 ensures decent parallelism on small machines; ceiling of 32
	// prevents socket/DB connection pool exhaustion on large ones.
	numWorkers := runtime.NumCPU() * 4
	if numWorkers < 8 {
		numWorkers = 8
	}
	if numWorkers > 32 {
		numWorkers = 32
	}

	report := &ReDiscoveryReport{
		NewModels:         make([]string, 0),
		ProviderBreakdown: make([]ProviderScanResult, 0),
		Errors:            make([]string, 0),
		StartedAt:         startTime,
		WorkerCount:       numWorkers,
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

	logger.Info("re-discovery: launching parallel provider scans",
		zap.Int("account_count", len(accounts)),
		zap.Int("worker_count", numWorkers),
		zap.Int("per_provider_timeout_sec", perProviderTimeoutSec),
	)

	// 3. Dispatch provider discovery tasks concurrently via bounded worker pool
	sem := semaphore.NewWeighted(int64(numWorkers))
	var wg sync.WaitGroup
	var mu sync.Mutex // guards report fields during concurrent writes

	for _, acc := range accounts {
		// Acquire a worker slot; if the parent context is cancelled, stop dispatching.
		if err := sem.Acquire(ctx, 1); err != nil {
			break
		}

		wg.Add(1)
		go func(acc providerAccount) {
			defer wg.Done()
			defer sem.Release(1)

			// Decrypt the API key (vault.Decrypt is stateless/AES-GCM — safe for concurrent use)
			apiKey, decErr := vault.Decrypt(acc.EncryptedKey)
			if decErr != nil {
				errMsg := fmt.Sprintf("%s(%s): decrypt error: %v", acc.Provider, acc.BaseURL, decErr)
				mu.Lock()
				report.Errors = append(report.Errors, errMsg)
				report.FailedEndpoints++
				report.ProviderBreakdown = append(report.ProviderBreakdown, ProviderScanResult{
					Provider: acc.Provider,
					BaseURL:  acc.BaseURL,
					Status:   "failed",
					Error:    errMsg,
				})
				mu.Unlock()
				return
			}

			weight := acc.Weight
			if weight <= 0 {
				weight = 1
			}

			// Per-provider isolated timeout: a dead/slow endpoint fails fast
			// without blocking the other goroutines.
			taskCtx, cancel := context.WithTimeout(ctx, perProviderTimeout)
			defer cancel()

			count, discovered, discErr := callProviderDiscovery(taskCtx, db, vault, acc, apiKey, weight)
			if discErr != nil {
				errMsg := fmt.Sprintf("%s(%s): %v", acc.Provider, acc.BaseURL, discErr)
				mu.Lock()
				report.Errors = append(report.Errors, errMsg)
				report.FailedEndpoints++
				report.ProviderBreakdown = append(report.ProviderBreakdown, ProviderScanResult{
					Provider: acc.Provider,
					BaseURL:  acc.BaseURL,
					Status:   "failed",
					Error:    errMsg,
				})
				mu.Unlock()
				logger.Warn("re-discovery: provider scan failed",
					zap.String("provider", acc.Provider),
					zap.String("base_url", acc.BaseURL),
					zap.Error(discErr),
				)
				return
			}

			// Identify which discovered models are actually new (O(1) map lookup)
			var newFromThisProvider []string
			for _, modelPattern := range discovered {
				if !existingPatterns[modelPattern] {
					newFromThisProvider = append(newFromThisProvider, modelPattern)
				}
			}

			mu.Lock()
			report.SuccessfulEndpoints++
			report.TotalModelsSynced += count
			report.ProviderBreakdown = append(report.ProviderBreakdown, ProviderScanResult{
				Provider:     acc.Provider,
				BaseURL:      acc.BaseURL,
				Status:       "success",
				ModelsSynced: count,
				NewModels:    newFromThisProvider,
			})
			mu.Unlock()

			logger.Info("re-discovery: provider scan completed",
				zap.String("provider", acc.Provider),
				zap.String("base_url", acc.BaseURL),
				zap.Int("synced", count),
				zap.Int("new", len(newFromThisProvider)),
			)
		}(acc)
	}

	// Wait for ALL goroutines to finish before building the final report
	wg.Wait()

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

	logger.Info("re-discovery: parallel scan complete",
		zap.Int("endpoints_scanned", report.TotalEndpointsScanned),
		zap.Int("successful", report.SuccessfulEndpoints),
		zap.Int("failed", report.FailedEndpoints),
		zap.Int("new_models", report.NewModelsAdded),
		zap.Int("total_synced", report.TotalModelsSynced),
		zap.Int("workers_used", numWorkers),
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

	case "zenmux":
		return DiscoverAndRegisterZenMuxModels(ctx, db, vault, apiKey, weight)

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
