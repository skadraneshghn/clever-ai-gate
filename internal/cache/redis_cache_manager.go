// Package cache provides Ristretto L1 and Redis L2 cache layers for Clever AI Gate.
// This file implements the RedisCacheManager: the authoritative store for all
// gateway-level cached data (active model lists, pool lists, provider lists).
//
// Architecture:
//
//	Request → Redis L2 (< 1ms) → Ristretto L1 (< 1µs) → PostgreSQL (fallback)
//
// Invalidation is event-driven:
//  1. Admin mutates data (create/update/delete pool, credential, provider)
//  2. Handler calls InvalidateAndPublish()
//  3. Redis DELs the stale keys + PUBLISHes on gateway:v1:events:cache_sync
//  4. ALL gateway replicas receive the Pub/Sub message and clear their Ristretto L1
//  5. Next request re-populates Redis L2 from PostgreSQL
package cache

import (
	"context"
	"encoding/json"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

const (
	// Redis key constants — versioned so key schema can evolve without manual flushes.
	KeyActiveModels = "gateway:v1:models:active"  // JSON []ActiveModelEntry
	KeyAllPools     = "gateway:v1:pools:all"       // JSON array of cached pool entries
	KeyAllProviders = "gateway:v1:providers:all"   // JSON array of cached providers

	// Pub/Sub channel for cross-node cache invalidation events.
	ChannelCacheSync = "gateway:v1:events:cache_sync"

	// DefaultCacheTTL is the maximum age of any cached entry.
	// Real invalidation is event-driven; this is just a safety-net for
	// edge cases where a publish message is lost.
	DefaultCacheTTL = 24 * time.Hour
)

// ActiveModelEntry is the Redis-serializable view of a model pool.
// Stored under KeyActiveModels as a JSON array.
type ActiveModelEntry struct {
	Pattern      string          `json:"id"`
	Capabilities map[string]bool `json:"capabilities,omitempty"`
}

// RedisCacheManager is the central Redis cache authority for Clever AI Gate.
// All public methods are nil-safe: when the manager is nil (Redis not configured)
// every operation is a silent no-op, preserving the existing PostgreSQL/Ristretto path.
type RedisCacheManager struct {
	rdb    *redis.Client
	logger *zap.Logger
	ctx    context.Context
	cancel context.CancelFunc
}

// NewRedisCacheManager creates a RedisCacheManager.
// Returns nil when rdb is nil — all callers must handle nil gracefully.
func NewRedisCacheManager(rdb *redis.Client, logger *zap.Logger) *RedisCacheManager {
	if rdb == nil {
		logger.Info("redis cache manager disabled — no Redis client")
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	logger.Info("redis cache manager initialized",
		zap.String("active_models_key", KeyActiveModels),
		zap.String("sync_channel", ChannelCacheSync),
	)
	return &RedisCacheManager{
		rdb:    rdb,
		logger: logger,
		ctx:    ctx,
		cancel: cancel,
	}
}

// GetJSON reads a JSON-encoded value from Redis and unmarshals it into target.
// Returns (true, nil) on cache hit, (false, nil) on cache miss, (false, err) on error.
// Nil-safe: returns (false, nil) when the manager is nil.
func (m *RedisCacheManager) GetJSON(ctx context.Context, key string, target interface{}) (bool, error) {
	if m == nil {
		return false, nil
	}

	rctx, cancel := context.WithTimeout(ctx, 150*time.Millisecond)
	defer cancel()

	val, err := m.rdb.Get(rctx, key).Result()
	if err == redis.Nil {
		return false, nil // cache miss
	}
	if err != nil {
		m.logger.Debug("redis cache read error", zap.String("key", key), zap.Error(err))
		return false, err
	}

	if err := json.Unmarshal([]byte(val), target); err != nil {
		m.logger.Warn("redis cache unmarshal error — treating as miss",
			zap.String("key", key),
			zap.Error(err),
		)
		return false, nil
	}

	return true, nil
}

// SetJSON marshals data as JSON and stores it in Redis with a TTL.
// The write is best-effort: errors are logged but not returned.
// Nil-safe: no-op when the manager is nil.
func (m *RedisCacheManager) SetJSON(ctx context.Context, key string, data interface{}, ttl time.Duration) {
	if m == nil {
		return
	}

	bytes, err := json.Marshal(data)
	if err != nil {
		m.logger.Warn("redis cache marshal error", zap.String("key", key), zap.Error(err))
		return
	}

	rctx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()

	if err := m.rdb.Set(rctx, key, bytes, ttl).Err(); err != nil {
		m.logger.Debug("redis cache write error (non-critical)", zap.String("key", key), zap.Error(err))
	}
}

// InvalidateAndPublish atomically deletes the given keys plus all
// tenant-scoped model keys, then publishes a "renew" event on the
// cache sync channel so every running gateway replica clears its
// local Ristretto L1 cache.
//
// This is the ONLY function that should be called after any admin
// mutation (pool create/update/delete, credential CRUD, provider registration).
// Nil-safe: no-op when the manager is nil.
func (m *RedisCacheManager) InvalidateAndPublish(ctx context.Context, keys ...string) {
	if m == nil {
		return
	}

	go func() {
		rctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()

		pipe := m.rdb.Pipeline()

		// Delete all explicitly named keys.
		for _, key := range keys {
			pipe.Del(rctx, key)
		}

		// Always invalidate both the pools and active models keys.
		pipe.Del(rctx, KeyActiveModels)
		pipe.Del(rctx, KeyAllPools)
		pipe.Del(rctx, KeyAllProviders)

		if _, err := pipe.Exec(rctx); err != nil {
			m.logger.Debug("redis cache invalidation pipeline error (non-critical)", zap.Error(err))
		}

		// Publish cross-node sync event.
		if err := m.rdb.Publish(rctx, ChannelCacheSync, "renew").Err(); err != nil {
			m.logger.Debug("redis cache sync publish error (non-critical)", zap.Error(err))
		}

		m.logger.Info("redis cache invalidated and sync event published",
			zap.Strings("keys", append(keys, KeyActiveModels, KeyAllPools, KeyAllProviders)),
		)
	}()
}

// SubscribeCacheSync starts a background goroutine that listens on the
// cache sync Pub/Sub channel. When a "renew" message arrives, onInvalidate
// is called — callers use this to clear their Ristretto L1 caches so the
// next request re-populates from Redis/PostgreSQL.
//
// The subscriber reconnects automatically after failures.
// Nil-safe: no-op when the manager is nil.
func (m *RedisCacheManager) SubscribeCacheSync(onInvalidate func()) {
	if m == nil {
		return
	}
	go m.subscribeLoop(onInvalidate)
	m.logger.Info("redis cache sync subscriber started", zap.String("channel", ChannelCacheSync))
}

func (m *RedisCacheManager) subscribeLoop(onInvalidate func()) {
	backoff := time.Second
	for {
		select {
		case <-m.ctx.Done():
			return
		default:
		}

		if err := m.subscribeCycle(onInvalidate); err != nil {
			m.logger.Warn("redis cache sync subscriber disconnected, reconnecting",
				zap.Error(err),
				zap.Duration("backoff", backoff),
			)
			select {
			case <-m.ctx.Done():
				return
			case <-time.After(backoff):
			}
			if backoff < 30*time.Second {
				backoff *= 2
			}
		} else {
			backoff = time.Second
		}
	}
}

func (m *RedisCacheManager) subscribeCycle(onInvalidate func()) error {
	sub := m.rdb.Subscribe(m.ctx, ChannelCacheSync)
	defer sub.Close() //nolint:errcheck

	// Confirm subscription.
	if _, err := sub.Receive(m.ctx); err != nil {
		return err
	}

	ch := sub.Channel()
	for {
		select {
		case <-m.ctx.Done():
			return nil
		case msg, ok := <-ch:
			if !ok {
				return nil
			}
			if msg.Payload == "renew" && onInvalidate != nil {
				m.logger.Info("received remote cache invalidation event — clearing local state")
				onInvalidate()
			}
		}
	}
}

// Stop shuts down the cache manager's background subscriber.
// Nil-safe.
func (m *RedisCacheManager) Stop() {
	if m == nil {
		return
	}
	m.cancel()
}
