package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/skadraneshghn/clever-ai-gate/internal/cache"
	"github.com/skadraneshghn/clever-ai-gate/internal/database"
)

// ProxyAuth extracts and validates the virtual API key from the Authorization header.
// All lookups are from Ristretto cache — zero database calls on the hot-path.
//
// Header format: Authorization: Bearer <virtual-api-key>
func ProxyAuth(cacheStore *cache.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "missing Authorization header",
			})
			return
		}

		// Extract bearer token
		if !strings.HasPrefix(authHeader, "Bearer ") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "invalid Authorization format, expected 'Bearer <key>'",
			})
			return
		}
		apiKey := authHeader[7:] // Strip "Bearer " prefix

		if apiKey == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "empty API key",
			})
			return
		}

		// Cache lookup — ~200ns via Ristretto
		tenantVal, found := cacheStore.Get(cache.TenantKey(apiKey))
		if !found {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "invalid API key",
			})
			return
		}

		tenant := tenantVal.(*database.TenantRow)

		// Check if tenant is active
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

		// Constant-time comparison to prevent timing attacks
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
