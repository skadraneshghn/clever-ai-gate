// Package redisclient provides a shared, tuned Redis client singleton for
// Clever AI Gate. All subsystems (rate limiter, cluster broadcaster, telemetry
// pipeline, tenant cache) share this single client to maximize connection reuse
// and avoid per-subsystem connection pool overhead.
package redisclient

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/skadraneshghn/clever-ai-gate/internal/config"
	"go.uber.org/zap"
)

// Client wraps the go-redis client with health state tracking.
type Client struct {
	rdb    *redis.Client
	logger *zap.Logger
	alive  bool
}

// New creates a connection-pool-tuned Redis client from config.
// Returns (nil, nil) when RedisURL is empty — all callers must handle nil gracefully,
// falling back to local in-memory alternatives.
func New(cfg *config.Config, logger *zap.Logger) (*Client, error) {
	if cfg.RedisURL == "" {
		logger.Info("no REDIS_URL configured — Redis features disabled")
		return nil, nil
	}

	opt, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		return nil, fmt.Errorf("invalid REDIS_URL: %w", err)
	}

	// Override pool settings with Clever Cloud-tuned values
	opt.PoolSize    = cfg.RedisPoolSize     // 200 concurrent connections
	opt.MinIdleConns = cfg.RedisMinIdle     // 20 warm pipes — eliminates connect latency on burst
	opt.ReadTimeout  = cfg.RedisReadTimeout  // 200ms — fall back to local on network blip
	opt.WriteTimeout = cfg.RedisWriteTimeout // 200ms
	opt.DialTimeout  = cfg.RedisDialTimeout  // 500ms
	opt.PoolTimeout  = 300 * time.Millisecond
	opt.MaxRetries   = 3
	opt.MinRetryBackoff = 8 * time.Millisecond
	opt.MaxRetryBackoff = 64 * time.Millisecond

	rdb := redis.NewClient(opt)

	// Verify connectivity at startup
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := rdb.Ping(ctx).Err(); err != nil {
		_ = rdb.Close()
		return nil, fmt.Errorf("redis ping failed: %w", err)
	}

	logger.Info("redis client connected",
		zap.String("addr", opt.Addr),
		zap.Int("pool_size", opt.PoolSize),
		zap.Int("min_idle", opt.MinIdleConns),
		zap.Duration("read_timeout", opt.ReadTimeout),
	)

	return &Client{rdb: rdb, logger: logger, alive: true}, nil
}

// Unwrap returns the underlying go-redis client. Callers MUST check for nil
// on the *Client itself before calling Unwrap.
func (c *Client) Unwrap() *redis.Client {
	if c == nil {
		return nil
	}
	return c.rdb
}

// Ping performs a health check against the Redis instance.
func (c *Client) Ping(ctx context.Context) error {
	if c == nil || c.rdb == nil {
		return fmt.Errorf("redis client not initialized")
	}
	return c.rdb.Ping(ctx).Err()
}

// Close shuts down the connection pool gracefully.
func (c *Client) Close() error {
	if c == nil || c.rdb == nil {
		return nil
	}
	return c.rdb.Close()
}

// PoolStats returns current connection pool statistics for observability.
func (c *Client) PoolStats() *redis.PoolStats {
	if c == nil || c.rdb == nil {
		return nil
	}
	return c.rdb.PoolStats()
}
