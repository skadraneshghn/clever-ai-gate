package middleware

import (
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
)

// RateLimiter implements per-tenant rate limiting using atomic counters
// with a sliding window approach.
//
// Design: Each tenant gets an entry with an atomic request counter and
// a window start timestamp. When the window expires, the counter resets.
// This avoids mutexes on the hot-path.
type RateLimiter struct {
	limiters sync.Map // map[string]*tenantLimiter
	defaultRPM int
}

type tenantLimiter struct {
	count       int64
	windowStart int64 // Unix nanosecond
	limit       int64
}

// NewRateLimiter creates a new rate limiter with the given default RPM.
func NewRateLimiter(defaultRPM int) *RateLimiter {
	return &RateLimiter{
		defaultRPM: defaultRPM,
	}
}

// Middleware returns a Gin middleware that enforces rate limits.
func (rl *RateLimiter) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		tenantID, exists := c.Get("tenant_id")
		if !exists {
			c.Next()
			return
		}

		// Get tenant-specific rate limit (set by auth middleware)
		rpmLimit := rl.defaultRPM
		if limit, ok := c.Get("tenant_rate_limit"); ok {
			rpmLimit = limit.(int)
		}

		key := tenantID.(string)
		now := time.Now().UnixNano()
		windowDuration := int64(time.Minute)

		// Get or create limiter for this tenant
		limiterVal, _ := rl.limiters.LoadOrStore(key, &tenantLimiter{
			count:       0,
			windowStart: now,
			limit:       int64(rpmLimit),
		})
		limiter := limiterVal.(*tenantLimiter)

		// Check if window has expired
		windowStart := atomic.LoadInt64(&limiter.windowStart)
		if now-windowStart > windowDuration {
			// Elect a single goroutine to perform window rotation via CAS.
			// Without CAS, multiple goroutines crossing the boundary simultaneously
			// would each reset the counter — wiping out counts recorded by others
			// and allowing tenants to exceed their hard quota limits.
			if atomic.CompareAndSwapInt64(&limiter.windowStart, windowStart, now) {
				atomic.StoreInt64(&limiter.count, 0)
				atomic.StoreInt64(&limiter.limit, int64(rpmLimit))
			}
		}

		// Increment counter
		count := atomic.AddInt64(&limiter.count, 1)
		limit := atomic.LoadInt64(&limiter.limit)

		if count > limit {
			// Calculate retry-after
			elapsed := time.Duration(now - atomic.LoadInt64(&limiter.windowStart))
			retryAfter := time.Minute - elapsed
			if retryAfter < 0 {
				retryAfter = time.Second
			}

			c.Header("Retry-After", fmt.Sprintf("%d", int(retryAfter.Seconds())))
			c.Header("X-RateLimit-Limit", fmt.Sprintf("%d", limit))
			c.Header("X-RateLimit-Remaining", "0")
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error":       "rate limit exceeded",
				"limit":       limit,
				"retry_after": int(retryAfter.Seconds()),
			})
			return
		}

		// Set rate limit headers
		c.Header("X-RateLimit-Limit", fmt.Sprintf("%d", limit))
		c.Header("X-RateLimit-Remaining", fmt.Sprintf("%d", limit-count))

		c.Next()
	}
}

// Cleanup removes expired limiters to prevent memory leaks.
// Should be called periodically (e.g., every 5 minutes).
func (rl *RateLimiter) Cleanup() {
	now := time.Now().UnixNano()
	windowDuration := int64(5 * time.Minute) // Keep limiters for 5 minutes

	rl.limiters.Range(func(key, value interface{}) bool {
		limiter := value.(*tenantLimiter)
		if now-atomic.LoadInt64(&limiter.windowStart) > windowDuration {
			rl.limiters.Delete(key)
		}
		return true
	})
}
