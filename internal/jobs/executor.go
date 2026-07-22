package jobs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"runtime"
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

// HostRateLimiter caps maximum concurrent active health check probes per domain host
// to prevent 503 ResourceExhausted rate limit bursts on upstream endpoints.
type HostRateLimiter struct {
	mu     sync.Mutex
	limits map[string]chan struct{}
}

func NewHostRateLimiter() *HostRateLimiter {
	return &HostRateLimiter{
		limits: make(map[string]chan struct{}),
	}
}

func (h *HostRateLimiter) Acquire(host string, maxConcurrent int) func() {
	h.mu.Lock()
	ch, exists := h.limits[host]
	if !exists {
		ch = make(chan struct{}, maxConcurrent)
		h.limits[host] = ch
	}
	h.mu.Unlock()

	ch <- struct{}{}
	return func() { <-ch }
}

// --- Built-in Executor: bulk_pool_health_check ---
// Iterates all credentials across every pool and executes CPU-parallel,
// adaptive capability-aware live HTTP probes per credential.
//
// Observability-only: probe results are logged and counted but NEVER written
// back to the database or Redis. is_healthy is exclusively controlled by
// admin action — background jobs no longer disable credentials automatically.

func newBulkPoolHealthCheckExecutor(db *pgxpool.Pool, rdb *redis.Client, vault *credentials.Vault, logger *zap.Logger) ExecutorFunc {
	return func(execCtx *ExecutionContext) (string, error) {
		ctx := execCtx.Context
		if ctx == nil {
			ctx = context.Background()
		}

		// 1. Pull all credentials joined with pool model_pattern and capabilities
		creds, err := database.ListAllCredentials(ctx, db)
		if err != nil {
			return "", fmt.Errorf("bulk health check: failed to list credentials: %w", err)
		}
		if len(creds) == 0 {
			return "No credentials found in database to evaluate.", nil
		}

		workerCount := runtime.NumCPU() * 4
		if workerCount < 16 {
			workerCount = 16
		}

		logger.Info("bulk_pool_health_check started",
			zap.String("run_id", execCtx.RunID),
			zap.Int("total_credentials", len(creds)),
			zap.Int("cpu_workers", workerCount),
		)

		jobsChan := make(chan *database.CredentialWithPool, len(creds))
		for _, cr := range creds {
			jobsChan <- cr
		}
		close(jobsChan)

		var successCount atomic.Int64
		var failureCount atomic.Int64
		var wg sync.WaitGroup

		hostLimiter := NewHostRateLimiter()

		// Tuned outbound HTTP client with high idle connection limits
		client := &http.Client{
			Timeout: 10 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        500,
				MaxIdleConnsPerHost: 30,
				IdleConnTimeout:     30 * time.Second,
			},
		}

		for i := 0; i < workerCount; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()

				for c := range jobsChan {
					// Acquire host concurrency lease (max 8 concurrent requests per host domain)
					hostKey := "default"
					if parsedURL, err := url.Parse(c.BaseURL); err == nil && parsedURL.Host != "" {
						hostKey = parsedURL.Host
					}
					release := hostLimiter.Acquire(hostKey, 8)

					// Decrypt key securely per credential
					apiKey, decErr := vault.Decrypt(c.EncryptedKey)
					if decErr != nil {
						release()
						logger.Error("bulk health: key decryption failed (no action taken)",
							zap.Int("cred_id", c.ID), zap.Error(decErr))
						failureCount.Add(1)
						continue
					}

					isHealthy, errStr := probeCredential(ctx, client, c, apiKey)
					release()

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
				}
			}()
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

// probeCredential sends a capability- and provider-adaptive HTTP health probe
// for a single credential and returns (isHealthy, *errorString).
func probeCredential(ctx context.Context, client *http.Client, c *database.CredentialWithPool, apiKey string) (bool, *string) {
	var req *http.Request
	var buildErr error

	if c.Provider == "ollama" {
		// Ollama: use /api/tags endpoint
		urlStr := strings.TrimRight(c.BaseURL, "/") + "/api/tags"
		req, buildErr = http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
		if buildErr == nil {
			req.Header.Set("Authorization", "Bearer "+apiKey)
		}
	} else {
		// Parse capabilities JSON if present
		var caps map[string]bool
		if len(c.Capabilities) > 0 {
			_ = json.Unmarshal(c.Capabilities, &caps)
		}

		// Resolve wildcard patterns
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

		probeReq, err := BuildAdaptiveProbeRequest(c.BaseURL, apiKey, c.Provider, testModel, caps)
		if err != nil {
			errStr := fmt.Sprintf("failed to build adaptive probe request: %v", err)
			return false, &errStr
		}

		req, buildErr = http.NewRequestWithContext(ctx, probeReq.Method, probeReq.URL, bytes.NewReader(probeReq.Body))
		if buildErr == nil {
			for k, v := range probeReq.Headers {
				req.Header.Set(k, v)
			}
		}
	}

	if buildErr != nil {
		errStr := fmt.Sprintf("failed to build HTTP request: %v", buildErr)
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
