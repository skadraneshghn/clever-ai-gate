package cache

import (
	"sync"
	"time"

	"github.com/dgraph-io/ristretto"
	"github.com/skadraneshghn/clever-ai-gate/internal/config"
	"go.uber.org/zap"
)

// Store wraps Ristretto with typed access methods for routing data.
// Ristretto uses TinyLFU admission policy which is ideal for our workload:
// popular models (gpt-4o, claude-sonnet) get prioritized in cache.
type Store struct {
	cache  *ristretto.Cache
	logger *zap.Logger
	mu     sync.RWMutex // protects pool map for atomic swaps
}

// New creates a new cache store backed by Ristretto.
func New(cfg *config.Config, logger *zap.Logger) (*Store, error) {
	cache, err := ristretto.NewCache(&ristretto.Config{
		NumCounters: cfg.CacheNumCounters,                        // ~10x max expected items
		MaxCost:     cfg.CacheMaxSizeMB * 1024 * 1024,            // MaxCost in bytes
		BufferItems: 64,                                          // Per-shard write buffer
		Metrics:     true,                                        // Enable hit/miss tracking
	})
	if err != nil {
		return nil, err
	}

	return &Store{
		cache:  cache,
		logger: logger,
	}, nil
}

// SetWithTTL stores a value in the cache with a time-to-live.
// cost should approximate the memory footprint of the value in bytes.
func (s *Store) SetWithTTL(key string, value interface{}, cost int64, ttl time.Duration) bool {
	return s.cache.SetWithTTL(key, value, cost, ttl)
}

// Set stores a value with no expiration.
func (s *Store) Set(key string, value interface{}, cost int64) bool {
	return s.cache.Set(key, value, cost)
}

// Get retrieves a value from the cache.
// Returns (value, true) on hit, (nil, false) on miss.
func (s *Store) Get(key string) (interface{}, bool) {
	return s.cache.Get(key)
}

// Del removes a value from the cache.
func (s *Store) Del(key string) {
	s.cache.Del(key)
}

// Clear removes all entries from the cache.
func (s *Store) Clear() {
	s.cache.Clear()
}

// Wait ensures all pending Set operations have been processed.
// Ristretto buffers writes — call this after bulk loading to guarantee visibility.
func (s *Store) Wait() {
	s.cache.Wait()
}

// Metrics returns cache hit/miss statistics.
func (s *Store) Metrics() *ristretto.Metrics {
	return s.cache.Metrics
}

// Close shuts down the cache.
func (s *Store) Close() {
	s.cache.Close()
}

// --- Typed key prefixes for cache namespacing ---

const (
	PrefixTenant = "t:"  // tenant API key → tenant data
	PrefixPool   = "p:"  // model pattern → balanced channel pool
	PrefixQuota  = "q:"  // tenant ID → quota metrics
)

// TenantKey returns the cache key for a tenant by API key.
func TenantKey(apiKey string) string {
	return PrefixTenant + apiKey
}

// PoolKey returns the cache key for a model routing pool.
func PoolKey(modelPattern string) string {
	return PrefixPool + modelPattern
}

// QuotaKey returns the cache key for tenant quota metrics.
func QuotaKey(tenantID string) string {
	return PrefixQuota + tenantID
}
