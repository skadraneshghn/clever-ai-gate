package middleware

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestRateLimiter_AllowsUnderLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rl := NewRateLimiter(100)

	allowed := 0
	for i := 0; i < 50; i++ {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set("tenant_id", "tenant-1")
		c.Set("tenant_rate_limit", 100)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

		rl.Middleware()(c)
		if w.Code != http.StatusOK {
			// c.Next() not called means middleware aborted
		}
		if !c.IsAborted() {
			allowed++
		}
	}

	if allowed != 50 {
		t.Errorf("expected 50 allowed, got %d", allowed)
	}
}

func TestRateLimiter_BlocksOverLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rl := NewRateLimiter(5)

	blocked := 0
	for i := 0; i < 20; i++ {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set("tenant_id", "tenant-block")
		c.Set("tenant_rate_limit", 5)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

		rl.Middleware()(c)
		if c.IsAborted() {
			blocked++
		}
	}

	if blocked != 15 {
		t.Errorf("expected 15 blocked, got %d", blocked)
	}
}

func TestRateLimiter_CASConcurrentWindowRotation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rl := NewRateLimiter(10000)

	const goroutines = 200
	const requestsPerGoroutine = 50

	var wg sync.WaitGroup
	var allowed int64
	var mu sync.Mutex

	// Pre-create the limiter entry so all goroutines hit the same window boundary
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("tenant_id", "tenant-cas")
	c.Set("tenant_rate_limit", 10000)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	rl.Middleware()(c)

	// Force the window to be expired by backdating the windowStart
	limiterVal, _ := rl.limiters.Load("tenant-cas")
	limiter := limiterVal.(*tenantLimiter)
	atomic.StoreInt64(&limiter.windowStart, time.Now().Add(-2*time.Minute).UnixNano())

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < requestsPerGoroutine; j++ {
				w := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(w)
				c.Set("tenant_id", "tenant-cas")
				c.Set("tenant_rate_limit", 10000)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

				rl.Middleware()(c)
				if !c.IsAborted() {
					mu.Lock()
					allowed++
					mu.Unlock()
				}
			}
		}()
	}
	wg.Wait()

	totalRequests := goroutines * requestsPerGoroutine

	// With the CAS fix, the window is rotated exactly once. Without CAS,
	// multiple goroutines could reset count to 0 mid-window, allowing more
	// requests than the limit. The limit is 10000 which is >= total requests,
	// so all should be allowed. The key assertion is that no panic occurs and
	// the count is consistent.
	if allowed != int64(totalRequests) {
		t.Errorf("expected %d allowed, got %d (possible count reset race)", totalRequests, allowed)
	}

	// Verify the counter was not double-reset: count should equal total requests
	finalCount := atomic.LoadInt64(&limiter.count)
	if finalCount != int64(totalRequests) {
		t.Errorf("expected counter %d, got %d (window was reset by multiple goroutines)", totalRequests, finalCount)
	}
}
