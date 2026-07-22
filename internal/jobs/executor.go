package jobs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/skadraneshghn/clever-ai-gate/internal/credentials"
	"github.com/skadraneshghn/clever-ai-gate/internal/database"
	"go.uber.org/zap"
)

// RegisterBuiltinExecutors registers all built-in job types into the registry.
// Call this once at startup before calling Scheduler.Start().
func RegisterBuiltinExecutors(reg *Registry, db *pgxpool.Pool, rdb *redis.Client, vault *credentials.Vault, logger *zap.Logger) {
	reg.Register("telemetry_cleanup", newTelemetryCleanupExecutor(db, logger))
	reg.Register("credential_health_check", newCredentialHealthCheckExecutor(db, logger))
	reg.Register("log_rotation", newLogRotationExecutor(logger))
	reg.Register("cache_warmup", newCacheWarmupExecutor(db, logger))
	reg.Register("job_log_cleanup", newJobLogCleanupExecutor(db, logger))
	reg.Register("noop", newNoopExecutor(logger))
	reg.Register("bulk_pool_health_check", newBulkPoolHealthCheckExecutor(db, rdb, vault, logger))
	reg.Register("provider_rediscovery", newProviderRediscoveryExecutor(db, vault, logger))

	logger.Info("built-in job executors registered",
		zap.Strings("types", reg.ListTypes()),
	)
}

// --- Built-in Executor: provider_rediscovery ---
// Scans all registered provider endpoints for newly available models and
// auto-registers them into the pool system. Produces a structured JSON report
// with counts of truly new models (diff-based) and per-provider breakdown.

func newProviderRediscoveryExecutor(db *pgxpool.Pool, vault *credentials.Vault, logger *zap.Logger) ExecutorFunc {
	return func(execCtx *ExecutionContext) (string, error) {
		// Use the scheduler's timeout-aware context, NOT context.Background().
		// context.Background() would detach from the scheduler timeout, leaving
		// goroutines running indefinitely after the job is marked as timed out.
		ctx := execCtx.Context
		if ctx == nil {
			ctx = context.Background() // safe fallback for tests / manual calls
		}

		// Allow per-provider timeout to be configured via job payload.
		// Default is 15 seconds (applied inside RunReDiscovery).
		perProviderTimeout := 0
		if v, ok := execCtx.Payload["per_provider_timeout_seconds"]; ok {
			if d, ok := v.(float64); ok && d > 0 {
				perProviderTimeout = int(d)
			}
		}

		logger.Info("provider_rediscovery job started",
			zap.String("run_id", execCtx.RunID),
		)

		report, err := credentials.RunReDiscovery(ctx, db, vault, logger, perProviderTimeout)
		if err != nil {
			return "", fmt.Errorf("re-discovery failed: %w", err)
		}

		// Broadcast reload to all cluster nodes so new pools are active immediately
		_, _ = db.Exec(ctx, "NOTIFY config_change, 'model_pools:reload'")

		logger.Info("provider_rediscovery job finished",
			zap.String("run_id", execCtx.RunID),
			zap.Int("new_models", report.NewModelsAdded),
			zap.Int("total_synced", report.TotalModelsSynced),
			zap.Int("workers_used", report.WorkerCount),
			zap.Int64("duration_ms", report.DurationMs),
		)

		return credentials.MarshalReport(report), nil
	}
}

// --- Built-in Executor: telemetry_cleanup ---
// Deletes request_logs older than the configured retention window.

func newTelemetryCleanupExecutor(db *pgxpool.Pool, logger *zap.Logger) ExecutorFunc {
	return func(ctx *ExecutionContext) (string, error) {
		retentionDays := 30
		if v, ok := ctx.Payload["retention_days"]; ok {
			if d, ok := v.(float64); ok && d > 0 {
				retentionDays = int(d)
			}
		}

		c := context.Background()
		result, err := db.Exec(c, `
			DELETE FROM request_logs
			WHERE created_at < NOW() - ($1 || ' days')::INTERVAL
		`, retentionDays)
		if err != nil {
			return "", fmt.Errorf("telemetry cleanup failed: %w", err)
		}

		deleted := result.RowsAffected()
		logger.Info("telemetry_cleanup complete",
			zap.Int64("deleted_rows", deleted),
			zap.Int("retention_days", retentionDays),
		)
		return fmt.Sprintf("Deleted %d telemetry records older than %d days", deleted, retentionDays), nil
	}
}

// --- Built-in Executor: credential_health_check ---
// Read-only audit of credential error state. is_healthy is exclusively
// controlled by admin action — this job no longer mutates it automatically.

func newCredentialHealthCheckExecutor(db *pgxpool.Pool, logger *zap.Logger) ExecutorFunc {
	return func(ctx *ExecutionContext) (string, error) {
		c := context.Background()

		// Count credentials that have a recorded error (read-only — no state mutation).
		var withErrors int64
		if err := db.QueryRow(c, `
			SELECT COUNT(*) FROM credentials
			WHERE last_error IS NOT NULL AND last_error != ''
		`).Scan(&withErrors); err != nil {
			return "", fmt.Errorf("credential health audit query failed: %w", err)
		}

		// Count credentials currently flagged healthy by admin.
		var healthy int64
		_ = db.QueryRow(c, `SELECT COUNT(*) FROM credentials WHERE is_healthy = TRUE`).Scan(&healthy)

		var total int64
		_ = db.QueryRow(c, `SELECT COUNT(*) FROM credentials`).Scan(&total)

		logger.Info("credential_health_check audit complete (read-only)",
			zap.Int64("total", total),
			zap.Int64("healthy", healthy),
			zap.Int64("with_errors", withErrors),
		)
		return fmt.Sprintf("Health audit: %d/%d healthy, %d with recorded errors (no state changes — admin controls is_healthy)",
			healthy, total, withErrors), nil
	}
}

// --- Built-in Executor: log_rotation ---
// Archives application log files.

func newLogRotationExecutor(logger *zap.Logger) ExecutorFunc {
	return func(ctx *ExecutionContext) (string, error) {
		// Log rotation is typically handled by the OS/logrotate daemon.
		// This executor signals any custom rotation logic.
		logger.Info("log_rotation job triggered")
		return "Log rotation signal sent. OS-level log rotation handles actual file management.", nil
	}
}

// --- Built-in Executor: cache_warmup ---
// Pre-warms the tenant cache from the database.

func newCacheWarmupExecutor(db *pgxpool.Pool, logger *zap.Logger) ExecutorFunc {
	return func(ctx *ExecutionContext) (string, error) {
		c := context.Background()

		var count int
		err := db.QueryRow(c, `SELECT COUNT(*) FROM tenants WHERE is_active = TRUE`).Scan(&count)
		if err != nil {
			return "", fmt.Errorf("cache warmup count query failed: %w", err)
		}

		logger.Info("cache_warmup triggered",
			zap.Int("active_tenants", count),
		)
		return fmt.Sprintf("Cache warmup triggered for %d active tenants (cache warming handled by sync manager)", count), nil
	}
}

// --- Built-in Executor: job_log_cleanup ---
// Removes old job run records from the database.

func newJobLogCleanupExecutor(db *pgxpool.Pool, logger *zap.Logger) ExecutorFunc {
	return func(ctx *ExecutionContext) (string, error) {
		retentionDays := 30
		if v, ok := ctx.Payload["retention_days"]; ok {
			if d, ok := v.(float64); ok && d > 0 {
				retentionDays = int(d)
			}
		}

		c := context.Background()
		cutoff := time.Now().AddDate(0, 0, -retentionDays)

		result, err := db.Exec(c, `
			DELETE FROM job_runs WHERE created_at < $1
		`, cutoff)
		if err != nil {
			return "", fmt.Errorf("job log cleanup failed: %w", err)
		}

		deleted := result.RowsAffected()
		logger.Info("job_log_cleanup complete",
			zap.Int64("deleted_runs", deleted),
			zap.Int("retention_days", retentionDays),
		)
		return fmt.Sprintf("Deleted %d job run records older than %d days", deleted, retentionDays), nil
	}
}

// --- Built-in Executor: noop ---
// Does nothing — useful for testing the scheduler pipeline.

func newNoopExecutor(logger *zap.Logger) ExecutorFunc {
	return func(ctx *ExecutionContext) (string, error) {
		logger.Debug("noop job executed", zap.String("job_id", ctx.JobID), zap.String("run_id", ctx.RunID))
		return "No operation performed (noop)", nil
	}
}

// isPermanentError checks if the error returned during health check indicates the API key is invalid/revoked/permanently blocked.
func isPermanentError(errStr string) bool {
	upper := strings.ToUpper(errStr)
	return strings.Contains(upper, "API_KEY_INVALID") ||
		strings.Contains(upper, "INVALID_API_KEY") ||
		strings.Contains(upper, "PERMISSION_DENIED") ||
		strings.Contains(upper, "FORBIDDEN") ||
		strings.Contains(upper, "HTTP 401") ||
		strings.Contains(upper, "HTTP 403") ||
		strings.Contains(upper, "DELETED")
}

// --- Built-in Executor: bulk_pool_health_check ---
// Iterates all credentials across every pool and executes a lightweight live
// HTTP probe per credential (bounded by a semaphore of 20 workers).
//
// Observability-only: probe results are logged and counted but NEVER written
// back to the database or Redis. is_healthy is exclusively controlled by
// admin action — background jobs no longer disable credentials automatically.

func newBulkPoolHealthCheckExecutor(db *pgxpool.Pool, rdb *redis.Client, vault *credentials.Vault, logger *zap.Logger) ExecutorFunc {
	return func(execCtx *ExecutionContext) (string, error) {
		ctx := context.Background()

		// 1. Pull all credentials joined with pool model_pattern
		creds, err := database.ListAllCredentials(ctx, db)
		if err != nil {
			return "", fmt.Errorf("bulk health check: failed to list credentials: %w", err)
		}
		if len(creds) == 0 {
			return "No credentials found in database to evaluate.", nil
		}

		logger.Info("bulk_pool_health_check started",
			zap.String("run_id", execCtx.RunID),
			zap.Int("total_credentials", len(creds)),
		)

		// 2. Bounded semaphore — max 20 parallel outbound connections
		const maxWorkers = 20
		sem := make(chan struct{}, maxWorkers)
		var wg sync.WaitGroup

		var successCount atomic.Int64
		var failureCount atomic.Int64

		client := &http.Client{Timeout: 10 * time.Second}

		for _, cr := range creds {
			wg.Add(1)
			sem <- struct{}{}

			go func(c *database.CredentialWithPool) {
				defer wg.Done()
				defer func() { <-sem }()

				// Decrypt key securely per credential
				apiKey, decErr := vault.Decrypt(c.EncryptedKey)
				if decErr != nil {
					logger.Error("bulk health: key decryption failed (no action taken)",
						zap.Int("cred_id", c.ID), zap.Error(decErr))
					failureCount.Add(1)
					return
				}

				isHealthy, errStr := probeCredential(ctx, client, c, apiKey)

				// Log the probe result for observability — NO DB or Redis writes.
				// is_healthy is exclusively controlled by admin action.
				if !isHealthy && errStr != nil {
					logger.Warn("bulk health: probe failed (no action taken — admin controls is_healthy)",
						zap.Int("cred_id", c.ID),
						zap.String("provider", c.Provider),
						zap.String("model_pattern", c.ModelPattern),
						zap.String("error", *errStr),
					)
					failureCount.Add(1)
				} else {
					successCount.Add(1)
				}
			}(cr)
		}

		wg.Wait()

		summary := fmt.Sprintf(
			"Bulk health probe complete — reachable: %d, unreachable: %d (total: %d). No state changes made.",
			successCount.Load(), failureCount.Load(), len(creds),
		)
		logger.Info("bulk_pool_health_check finished",
			zap.String("run_id", execCtx.RunID),
			zap.Int64("reachable", successCount.Load()),
			zap.Int64("unreachable", failureCount.Load()),
		)
		return summary, nil
	}
}

// probeCredential sends a lightweight HTTP health probe to the upstream provider
// for a single credential and returns (isHealthy, *errorString).
func probeCredential(ctx context.Context, client *http.Client, c *database.CredentialWithPool, apiKey string) (bool, *string) {
	var req *http.Request
	var buildErr error

	if c.Provider == "ollama" {
		// Ollama: use /api/tags instead of chat completions
		url := strings.TrimRight(c.BaseURL, "/") + "/api/tags"
		req, buildErr = http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if buildErr == nil {
			req.Header.Set("Authorization", "Bearer "+apiKey)
		}
	} else {
		// All other providers: POST /v1/chat/completions with a minimal payload
		url := strings.TrimRight(c.BaseURL, "/")
		if !strings.HasSuffix(url, "/v1") && c.Provider != "custom" {
			url += "/v1"
		}
		url += "/chat/completions"

		// Resolve wildcard model patterns to a concrete, tiny target model
		testModel := c.ModelPattern
		if strings.Contains(testModel, "*") {
			switch {
			case strings.Contains(testModel, "gpt"):
				testModel = "gpt-4o-mini"
			case strings.Contains(testModel, "claude"):
				testModel = "claude-3-5-haiku-20241022"
			case strings.Contains(testModel, "nvidia"):
				testModel = "nvidia/llama-3.1-nemotron-70b-instruct"
			default:
				testModel = strings.ReplaceAll(testModel, "*", "latest")
			}
		}
		testModel = strings.TrimPrefix(testModel, "nvidia/")
		testModel = strings.TrimPrefix(testModel, "ollama/")

		payload := map[string]interface{}{
			"model": testModel,
			"messages": []map[string]string{
				{"role": "user", "content": "Reply with exactly: OK"},
			},
			"temperature": 0,
			"max_tokens":  2,
		}
		bodyBytes, _ := json.Marshal(payload)
		req, buildErr = http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
		if buildErr == nil {
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+apiKey)
		}
	}

	if buildErr != nil {
		errStr := fmt.Sprintf("failed to build probe request: %v", buildErr)
		return false, &errStr
	}

	resp, doErr := client.Do(req)
	if doErr != nil {
		errStr := fmt.Sprintf("connection error: %v", doErr)
		return false, &errStr
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return true, nil
	}

	limitReader := io.LimitReader(resp.Body, 512)
	respBytes, _ := io.ReadAll(limitReader)
	errStr := fmt.Sprintf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(respBytes)))
	return false, &errStr
}
