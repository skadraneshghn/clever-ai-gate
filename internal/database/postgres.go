package database

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

// NewPool creates a pgx connection pool optimized for the gateway workload.
//
// Configuration rationale:
//   - MaxConns=20: Database is NEVER on the hot-path. Only used for:
//     admin API CRUD, telemetry bulk writes, and config reload.
//   - MinConns=5: Keep warm connections for telemetry flush workers.
//   - HealthCheckPeriod=30s: Detect dead connections quickly for LISTEN/NOTIFY.
func NewPool(ctx context.Context, databaseURL string, logger *zap.Logger) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse database URL: %w", err)
	}

	config.MaxConns = 20
	config.MinConns = 5
	config.MaxConnLifetime = 1 * time.Hour
	config.MaxConnIdleTime = 30 * time.Minute
	config.HealthCheckPeriod = 30 * time.Second

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection pool: %w", err)
	}

	// Verify connectivity
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	// Check if pgvector is available
	var hasVector bool
	err = pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_type WHERE typname = 'vector')").Scan(&hasVector)
	if err == nil {
		HasPgVector = hasVector
	} else {
		HasPgVector = false
	}

	logger.Info("database connection pool established",
		zap.Int32("max_conns", config.MaxConns),
		zap.Int32("min_conns", config.MinConns),
		zap.Bool("has_pgvector", HasPgVector),
	)

	return pool, nil
}

// HasPgVector is set during pool initialization indicating if pgvector is active.
var HasPgVector bool

