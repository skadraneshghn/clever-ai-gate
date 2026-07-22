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
// High-Performance Architecture:
// 1. Decoupled Network I/O: Workers run pure HTTP API calls in parallel across CPU cores. No DB connections or locks are held during fetching.
// 2. In-Memory Aggregation: Discovered models are collected into a thread-safe channel/slice.
// 3. Single Batch Transaction: All items are bulk-upserted into PostgreSQL in a SINGLE transaction with sorted keys to completely eliminate deadlocks (SQLSTATE 40P01).
// 4. Single NOTIFY: Fires 'model_pools:reload' exactly ONCE at job completion.
func RunReDiscovery(ctx context.Context, db *pgxpool.Pool, vault *Vault, logger *zap.Logger, perProviderTimeoutSec int) (*ReDiscoveryReport, error) {
	startTime := time.Now()

	if perProviderTimeoutSec <= 0 {
		perProviderTimeoutSec = 15
	}
	perProviderTimeout := time.Duration(perProviderTimeoutSec) * time.Second

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

	// 4. Batch persist all collected discovered items in a single transaction with sorted keys
	if len(allDiscoveredItems) > 0 {
		totalSynced, newAdded, batchErr := BatchInsertDiscoveredModels(ctx, db, vault, allDiscoveredItems)
		if batchErr != nil {
			logger.Error("re-discovery: batch insert failed", zap.Error(batchErr))
			report.Errors = append(report.Errors, fmt.Sprintf("batch insert error: %v", batchErr))
		} else {
			report.TotalModelsSynced = totalSynced
			report.NewModelsAdded = newAdded
		}
	}

	// 5. Post-scan diff: query model_pools again and find truly new patterns
	postPatterns, err := snapshotModelPatterns(ctx, db)
	if err != nil {
		logger.Warn("re-discovery: failed to snapshot post-scan patterns", zap.Error(err))
	} else {
		for pattern := range postPatterns {
			if !existingPatterns[pattern] {
				report.NewModels = append(report.NewModels, pattern)
			}
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
