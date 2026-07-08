package cache

import (
	"context"
	"encoding/json"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/skadraneshghn/clever-ai-gate/internal/database"
	"go.uber.org/zap"
)

const (
	tenantCacheTTL    = 5 * time.Minute
	tenantRedisPrefix = "clever_gate:tenant:"
)

// RedisTenantCache implements a two-layer tenant lookup cache:
//   - L1: Ristretto (sub-microsecond, in-process)
//   - L2: Redis (single-millisecond, cross-node shared)
//   - L3: PostgreSQL (fallback, only on cold start)
//
// Reads are always served from L1 if available, making the hot-path
// completely free of network I/O. Redis is only consulted on L1 miss,
// and writes to Redis happen asynchronously via fire-and-forget goroutines.
type RedisTenantCache struct {
	l1     *Store
	l2     *redis.Client
	logger *zap.Logger
}

// NewRedisTenantCache creates the two-layer cache.
// l2 may be nil — in that case only L1 (Ristretto) is used.
func NewRedisTenantCache(l1 *Store, l2 *redis.Client, logger *zap.Logger) *RedisTenantCache {
	return &RedisTenantCache{l1: l1, l2: l2, logger: logger}
}

// Get looks up a tenant by API key.
// Returns (tenant, true) on hit; (nil, false) on miss (DB lookup required).
// Hot-path: always checks L1 first — zero network I/O when hit.
func (c *RedisTenantCache) Get(ctx context.Context, apiKey string) (*database.TenantRow, bool) {
	// L1: Ristretto (sub-microsecond)
	if val, ok := c.l1.Get(TenantKey(apiKey)); ok {
		return val.(*database.TenantRow), true
	}

	// L2: Redis (milliseconds)
	if c.l2 != nil {
		tenant := c.getFromRedis(ctx, apiKey)
		if tenant != nil {
			// Populate L1 for subsequent requests
			c.l1.SetWithTTL(TenantKey(apiKey), tenant, 200, tenantCacheTTL)
			return tenant, true
		}
	}

	return nil, false
}

// Set stores a tenant in both L1 and L2 asynchronously.
// The L1 write is synchronous (required for immediate consistency on this node).
// The Redis L2 write is a fire-and-forget goroutine — never blocks the caller.
func (c *RedisTenantCache) Set(apiKey string, tenant *database.TenantRow) {
	// L1: synchronous — must be visible immediately on this node
	c.l1.SetWithTTL(TenantKey(apiKey), tenant, 200, tenantCacheTTL)

	// L2: async — other nodes benefit on their next L1 miss
	if c.l2 != nil {
		go func() {
			data, err := json.Marshal(tenant)
			if err != nil {
				return
			}
			ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
			defer cancel()
			key := tenantRedisPrefix + apiKey
			c.l2.Set(ctx, key, data, tenantCacheTTL) //nolint:errcheck
		}()
	}
}

// Invalidate removes a tenant from both L1 and L2.
// L1 deletion is synchronous. Redis DEL is async.
func (c *RedisTenantCache) Invalidate(apiKey string) {
	// L1: synchronous
	c.l1.Del(TenantKey(apiKey))

	// L2: async
	if c.l2 != nil {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
			defer cancel()
			c.l2.Del(ctx, tenantRedisPrefix+apiKey) //nolint:errcheck
		}()
	}
}

func (c *RedisTenantCache) getFromRedis(ctx context.Context, apiKey string) *database.TenantRow {
	rctx, cancel := context.WithTimeout(ctx, 150*time.Millisecond)
	defer cancel()

	data, err := c.l2.Get(rctx, tenantRedisPrefix+apiKey).Bytes()
	if err != nil {
		return nil // miss or error — fall through to DB
	}

	var tenant database.TenantRow
	if err := json.Unmarshal(data, &tenant); err != nil {
		c.logger.Debug("failed to unmarshal tenant from redis", zap.Error(err))
		return nil
	}
	return &tenant
}
