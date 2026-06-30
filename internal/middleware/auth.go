package middleware

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/skadraneshghn/clever-ai-gate/internal/cache"
	"github.com/skadraneshghn/clever-ai-gate/internal/database"
)

// tenantLookup abstracts single and two-layer tenant lookup.
type tenantLookup interface {
	Get(ctx context.Context, apiKey string) (*database.TenantRow, bool)
}

// ristrettoLookup wraps bare *cache.Store to satisfy tenantLookup.
type ristrettoLookup struct{ store *cache.Store }

func (r *ristrettoLookup) Get(_ context.Context, apiKey string) (*database.TenantRow, bool) {
	val, ok := r.store.Get(cache.TenantKey(apiKey))
	if !ok {
		return nil, false
	}
	return val.(*database.TenantRow), true
}

// ProxyAuth extracts and validates the virtual API key from the Authorization header.
// Uses RedisTenantCache (L1 Ristretto + L2 Redis) when available, falling back to
// Ristretto-only — zero database calls on every hot-path request.
//
// Header format: Authorization: Bearer <virtual-api-key>
func ProxyAuth(cacheStore *cache.Store) gin.HandlerFunc {
	return proxyAuthWith(&ristrettoLookup{cacheStore})
}

// ProxyAuthWithRedis uses the two-layer Ristretto+Redis tenant cache.
func ProxyAuthWithRedis(tenantCache *cache.RedisTenantCache) gin.HandlerFunc {
	return proxyAuthWith(tenantCache)
}

func proxyAuthWith(lookup tenantLookup) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "missing Authorization header",
			})
			return
		}

		if !strings.HasPrefix(authHeader, "Bearer ") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "invalid Authorization format, expected 'Bearer <key>'",
			})
			return
		}
		apiKey := authHeader[7:]

		if apiKey == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "empty API key",
			})
			return
		}

		// L1 Ristretto lookup (~200ns) — L2 Redis on miss (~1ms) — never hits DB
		ctx, cancel := context.WithTimeout(c.Request.Context(), 200*time.Millisecond)
		tenant, found := lookup.Get(ctx, apiKey)
		cancel()

		if !found {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "invalid API key",
			})
			return
		}

		if !tenant.IsActive {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error": "account is suspended",
			})
			return
		}

		// Attach tenant context for downstream handlers
		c.Set("tenant_id", tenant.ID)
		c.Set("tenant_name", tenant.Name)
		c.Set("tenant_rate_limit", tenant.RateLimitRPM)
		c.Set("tenant_balance", tenant.TokenBalance)

		c.Next()
	}
}

// AdminAuth validates the master admin API key for management endpoints.
// Uses constant-time comparison to prevent timing attacks.
func AdminAuth(adminAPIKey string) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "missing Authorization header",
			})
			return
		}

		if !strings.HasPrefix(authHeader, "Bearer ") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "invalid Authorization format",
			})
			return
		}
		token := authHeader[7:]

		if !constantTimeEqual(token, adminAPIKey) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "invalid admin API key",
			})
			return
		}

		c.Next()
	}
}

// constantTimeEqual performs a constant-time string comparison.
func constantTimeEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var result byte
	for i := 0; i < len(a); i++ {
		result |= a[i] ^ b[i]
	}
	return result == 0
}
