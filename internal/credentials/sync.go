package credentials

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/skadraneshghn/clever-ai-gate/internal/cache"
	"github.com/skadraneshghn/clever-ai-gate/internal/database"
	"go.uber.org/zap"
)

// SyncManager handles real-time configuration synchronization between
// PostgreSQL and the in-memory routing cache using LISTEN/NOTIFY.
//
// When an admin updates a config via the management API, PostgreSQL triggers
// fire a notification. This manager receives it and atomically swaps the
// affected routing pool in cache — zero downtime, zero lock contention.
type SyncManager struct {
	pool   *pgxpool.Pool
	cache  *cache.Store
	vault  *Vault
	logger *zap.Logger
	pools  atomic.Pointer[map[string]*BalancedChannelPool] // Atomic pointer for lock-free reads
	ctx    context.Context
	cancel context.CancelFunc
}

// NewSyncManager creates a new configuration sync manager.
func NewSyncManager(pool *pgxpool.Pool, cacheStore *cache.Store, vault *Vault, logger *zap.Logger) *SyncManager {
	ctx, cancel := context.WithCancel(context.Background())
	sm := &SyncManager{
		pool:   pool,
		cache:  cacheStore,
		vault:  vault,
		logger: logger,
		ctx:    ctx,
		cancel: cancel,
	}
	// Initialize with empty map
	emptyMap := make(map[string]*BalancedChannelPool)
	sm.pools.Store(&emptyMap)
	return sm
}

// LoadInitialState loads all routing pools from the database into cache.
// Called once at startup before the server starts accepting requests.
func (sm *SyncManager) LoadInitialState(ctx context.Context) error {
	poolRows, credsByPool, err := database.LoadAllPoolsWithCredentials(ctx, sm.pool)
	if err != nil {
		return fmt.Errorf("failed to load initial state: %w", err)
	}

	poolMap := make(map[string]*BalancedChannelPool)

	for _, pr := range poolRows {
		creds := credsByPool[pr.ID]
		runtimeCreds := make([]*RuntimeCredential, 0, len(creds))

		for _, cr := range creds {
			if !cr.IsHealthy {
				continue
			}
			// Decrypt the API key
			decryptedKey, err := sm.vault.Decrypt(cr.EncryptedKey)
			if err != nil {
				sm.logger.Error("failed to decrypt credential",
					zap.Int("credential_id", cr.ID),
					zap.Error(err),
				)
				continue
			}

			runtimeCreds = append(runtimeCreds, &RuntimeCredential{
				ID:       cr.ID,
				Provider: cr.Provider,
				APIKey:   decryptedKey,
				BaseURL:  cr.BaseURL,
				Weight:   cr.Weight,
			})
		}

		if len(runtimeCreds) == 0 {
			sm.logger.Warn("pool has no healthy credentials",
				zap.String("model", pr.ModelPattern),
			)
			continue
		}

		pool := NewBalancedPool(pr.ModelPattern, pr.Strategy, runtimeCreds, nil)
		poolMap[pr.ModelPattern] = pool

		// Store in cache for fast lookup
		sm.cache.Set(cache.PoolKey(pr.ModelPattern), pool, int64(len(runtimeCreds)*100))
	}

	// Wire up fallback pools (second pass after all pools are created)
	for _, pr := range poolRows {
		if pr.FallbackPoolID != nil {
			// Find the fallback pool
			for _, fbRow := range poolRows {
				if fbRow.ID == *pr.FallbackPoolID {
					if primaryPool, ok := poolMap[pr.ModelPattern]; ok {
						if fbPool, ok := poolMap[fbRow.ModelPattern]; ok {
							primaryPool.FallbackPool = fbPool
						}
					}
					break
				}
			}
		}
	}

	// Cache active models list for /v1/models endpoint
	var activeModels []string
	for m := range poolMap {
		activeModels = append(activeModels, m)
	}
	sm.cache.Set("system:active_models", activeModels, 1000)

	sm.pools.Store(&poolMap)
	sm.cache.Wait() // Ensure all cache writes are visible

	sm.logger.Info("initial routing state loaded",
		zap.Int("pool_count", len(poolMap)),
	)

	// Also load all tenants into cache
	return sm.loadTenants(ctx)
}

// loadTenants loads all active tenants into cache.
func (sm *SyncManager) loadTenants(ctx context.Context) error {
	tenants, err := database.ListTenants(ctx, sm.pool)
	if err != nil {
		return fmt.Errorf("failed to load tenants: %w", err)
	}

	for _, t := range tenants {
		if t.IsActive {
			sm.cache.Set(cache.TenantKey(t.APIKey), t, 200)
		}
	}
	sm.cache.Wait()

	sm.logger.Info("tenants loaded into cache", zap.Int("count", len(tenants)))
	return nil
}

// StartListener starts the PostgreSQL LISTEN/NOTIFY watcher.
// Runs in a background goroutine and hot-reloads affected pools
// when configuration changes are detected.
//
// Gap 3 Fix: The listener goroutine is wrapped in a panic defender
// to prevent config reload errors from crashing the gateway container.
func (sm *SyncManager) StartListener() {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				sm.logger.Error("recovered from LISTEN/NOTIFY listener panic",
					zap.Any("panic", r),
				)
				// Restart the listener to maintain config sync
				time.Sleep(5 * time.Second)
				go sm.listenLoop()
			}
		}()
		sm.listenLoop()
	}()
	sm.logger.Info("LISTEN/NOTIFY watcher started")
}

func (sm *SyncManager) listenLoop() {
	for {
		select {
		case <-sm.ctx.Done():
			return
		default:
		}

		err := sm.listen()
		if err != nil {
			sm.logger.Error("LISTEN/NOTIFY connection error, reconnecting",
				zap.Error(err),
			)
			time.Sleep(5 * time.Second) // Backoff before reconnect
		}
	}
}

func (sm *SyncManager) listen() error {
	conn, err := sm.pool.Acquire(sm.ctx)
	if err != nil {
		return fmt.Errorf("failed to acquire connection: %w", err)
	}
	defer conn.Release()

	_, err = conn.Exec(sm.ctx, "LISTEN config_change")
	if err != nil {
		return fmt.Errorf("failed to LISTEN: %w", err)
	}

	for {
		notification, err := conn.Conn().WaitForNotification(sm.ctx)
		if err != nil {
			return fmt.Errorf("notification wait failed: %w", err)
		}

		sm.logger.Info("config change notification received",
			zap.String("payload", notification.Payload),
		)

		// Parse payload: "table_name:id"
		parts := strings.SplitN(notification.Payload, ":", 2)
		if len(parts) != 2 {
			continue
		}

		tableName := parts[0]

		// Reload the affected data
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		switch tableName {
		case "model_pools", "credentials":
			if err := sm.reloadPools(ctx); err != nil {
				sm.logger.Error("failed to reload pools", zap.Error(err))
			}
		case "tenants":
			if err := sm.loadTenants(ctx); err != nil {
				sm.logger.Error("failed to reload tenants", zap.Error(err))
			}
		}
		cancel()
	}
}

// reloadPools performs a full reload of all routing pools.
// Uses atomic pointer swap for zero-downtime updates.
func (sm *SyncManager) reloadPools(ctx context.Context) error {
	if err := sm.LoadInitialState(ctx); err != nil {
		return err
	}
	sm.logger.Info("routing pools hot-reloaded")
	return nil
}

// Stop shuts down the sync manager.
func (sm *SyncManager) Stop() {
	sm.cancel()
}

// GetPool returns a routing pool by model pattern (for direct access).
func (sm *SyncManager) GetPool(model string) *BalancedChannelPool {
	pools := sm.pools.Load()
	if pools == nil {
		return nil
	}
	return (*pools)[model]
}
