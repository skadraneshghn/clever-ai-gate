package credentials

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
	"golang.org/x/sync/semaphore"
)

// ReDiscoveryReport holds the aggregated results of a full provider re-discovery scan.
// Serialized to JSON and stored in job_runs.output by the scheduler framework.
type ReDiscoveryReport struct {
	TotalEndpointsScanned int                  `json:"total_endpoints_scanned"`
	SuccessfulEndpoints   int                  `json:"successful_endpoints"`
	FailedEndpoints       int                  `json:"failed_endpoints"`
	NewModelsAdded        int                  `json:"new_models_added"`
	TotalModelsSynced     int                  `json:"total_models_synced"`
	NewModels             []string             `json:"new_models"`
	ProviderBreakdown     []ProviderScanResult `json:"provider_breakdown"`
	Errors                []string             `json:"errors,omitempty"`
	StartedAt             time.Time            `json:"started_at"`
	CompletedAt           time.Time            `json:"completed_at"`
	DurationMs            int64                `json:"duration_ms"`
	WorkerCount           int                  `json:"worker_count"`
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
// Ultra-High Performance Architecture:
// 1. 50 Parallel Workers (HTTP ONLY): Workers run pure HTTP API calls in parallel. Zero DB locks or connections held during fetching.
// 2. Fast Fail (3s Timeout): Unresponsive provider endpoints fast-fail in 3s so the whole job completes in ~2.5–5 seconds.
// 3. In-Memory Deduplication & Filter: Pre-loads existing model patterns. If 0 new models are found, NO DB transaction or NOTIFY is executed!
// 4. Single Batch Transaction: All genuinely new items are bulk-upserted into PostgreSQL in a SINGLE transaction with sorted keys to eliminate deadlocks (SQLSTATE 40P01).
// 5. Single NOTIFY: Fires 'model_pools:reload' exactly ONCE at job completion only when new models were added.
func RunReDiscovery(ctx context.Context, db *pgxpool.Pool, vault *Vault, logger *zap.Logger, perProviderTimeoutSec int) (*ReDiscoveryReport, error) {
	startTime := time.Now()

	// Strict fast-fail timeout: default 3 seconds per provider HTTP call
	if perProviderTimeoutSec <= 0 {
		perProviderTimeoutSec = 3
	}
	perProviderTimeout := time.Duration(perProviderTimeoutSec) * time.Second

	// High worker concurrency (50 workers) since workers ONLY perform HTTP network I/O
	numWorkers := 50
	if c := runtime.NumCPU() * 8; c > numWorkers {
		numWorkers = c
	}
	if numWorkers > 60 {
		numWorkers = 60
	}

	report := &ReDiscoveryReport{
		NewModels:         make([]string, 0),
		ProviderBreakdown: make([]ProviderScanResult, 0),
		Errors:            make([]string, 0),
		StartedAt:         startTime,
		WorkerCount:       numWorkers,
	}

	// 1. Snapshot all existing model_patterns before discovery (1 SELECT query)
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
	var mu sync.Mutex

	var allDiscoveredItems []DiscoveredModelItem

	for _, acc := range accounts {
		if err := sem.Acquire(ctx, 1); err != nil {
			break
		}

		wg.Add(1)
		go func(acc providerAccount) {
			defer wg.Done()
			defer sem.Release(1)

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

			taskCtx, cancel := context.WithTimeout(ctx, perProviderTimeout)
			defer cancel()

			discoveredItems, fetchErr := fetchProviderDiscoveredModels(taskCtx, acc, apiKey, weight)
			if fetchErr != nil {
				errMsg := fmt.Sprintf("%s(%s): %v", acc.Provider, acc.BaseURL, fetchErr)
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
					zap.Error(fetchErr),
				)
				return
			}

			mu.Lock()
			report.SuccessfulEndpoints++
			allDiscoveredItems = append(allDiscoveredItems, discoveredItems...)
			report.ProviderBreakdown = append(report.ProviderBreakdown, ProviderScanResult{
				Provider:     acc.Provider,
				BaseURL:      acc.BaseURL,
				Status:       "success",
				ModelsSynced: len(discoveredItems),
			})
			mu.Unlock()

			logger.Info("re-discovery: provider scan completed",
				zap.String("provider", acc.Provider),
				zap.String("base_url", acc.BaseURL),
				zap.Int("synced", len(discoveredItems)),
			)
		}(acc)
	}

	wg.Wait()

	// 4. In-Memory Filtering against existing model snapshot
	var trulyNewItems []DiscoveredModelItem
	seenPatterns := make(map[string]bool)

	for _, item := range allDiscoveredItems {
		report.TotalModelsSynced++
		if !existingPatterns[item.ModelPattern] && !seenPatterns[item.ModelPattern] {
			seenPatterns[item.ModelPattern] = true
			trulyNewItems = append(trulyNewItems, item)
			report.NewModels = append(report.NewModels, item.ModelPattern)
		}
	}

	// 5. IF NO NEW MODELS: Complete immediately without executing ANY database transactions or NOTIFY signals!
	if len(trulyNewItems) == 0 {
		logger.Info("re-discovery: complete - no new models found (0 DB writes executed)")
	} else {
		// Only run batch transaction if new models were discovered
		totalSynced, newAdded, batchErr := BatchInsertDiscoveredModels(ctx, db, vault, trulyNewItems)
		if batchErr != nil {
			logger.Error("re-discovery: batch insert failed", zap.Error(batchErr))
			report.Errors = append(report.Errors, fmt.Sprintf("batch insert error: %v", batchErr))
		} else {
			report.TotalModelsSynced = totalSynced
			report.NewModelsAdded = newAdded
		}
	}

	report.NewModelsAdded = len(report.NewModels)
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

// MarshalReport serializes a ReDiscoveryReport to a JSON string suitable for job_runs.output storage.
func MarshalReport(report *ReDiscoveryReport) string {
	data, err := json.Marshal(report)
	if err != nil {
		return fmt.Sprintf(`{"error":"failed to marshal report: %s"}`, err.Error())
	}
	return string(data)
}
