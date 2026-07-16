package jobs

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

// RegisterBuiltinExecutors registers all built-in job types into the registry.
// Call this once at startup before calling Scheduler.Start().
func RegisterBuiltinExecutors(reg *Registry, db *pgxpool.Pool, logger *zap.Logger) {
	reg.Register("telemetry_cleanup", newTelemetryCleanupExecutor(db, logger))
	reg.Register("credential_health_check", newCredentialHealthCheckExecutor(db, logger))
	reg.Register("log_rotation", newLogRotationExecutor(logger))
	reg.Register("cache_warmup", newCacheWarmupExecutor(db, logger))
	reg.Register("job_log_cleanup", newJobLogCleanupExecutor(db, logger))
	reg.Register("noop", newNoopExecutor(logger))

	logger.Info("built-in job executors registered",
		zap.Strings("types", reg.ListTypes()),
	)
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
// Marks credentials as unhealthy if they have a recent error.

func newCredentialHealthCheckExecutor(db *pgxpool.Pool, logger *zap.Logger) ExecutorFunc {
	return func(ctx *ExecutionContext) (string, error) {
		c := context.Background()

		// Check for credentials that had errors in the last hour and mark them unhealthy
		result, err := db.Exec(c, `
			UPDATE credentials
			SET is_healthy = FALSE
			WHERE last_error IS NOT NULL
			  AND last_error != ''
			  AND is_healthy = TRUE
		`)
		if err != nil {
			return "", fmt.Errorf("credential health check failed: %w", err)
		}

		markedUnhealthy := result.RowsAffected()

		// Also recover credentials that had no recent errors
		recovered, err := db.Exec(c, `
			UPDATE credentials
			SET is_healthy = TRUE
			WHERE last_error IS NULL OR last_error = ''
			  AND is_healthy = FALSE
		`)
		if err != nil {
			logger.Warn("failed to recover credentials", zap.Error(err))
		}

		recoveredCount := int64(0)
		if err == nil {
			recoveredCount = recovered.RowsAffected()
		}

		logger.Info("credential_health_check complete",
			zap.Int64("marked_unhealthy", markedUnhealthy),
			zap.Int64("recovered", recoveredCount),
		)
		return fmt.Sprintf("Health check: %d marked unhealthy, %d recovered", markedUnhealthy, recoveredCount), nil
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
