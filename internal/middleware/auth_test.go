package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/skadraneshghn/clever-ai-gate/internal/cache"
	"github.com/skadraneshghn/clever-ai-gate/internal/config"
	"github.com/skadraneshghn/clever-ai-gate/internal/database"
	"go.uber.org/zap"
)

func TestProxyAuth_MasterAdminKey(t *testing.T) {
	gin.SetMode(gin.TestMode)

	logger := zap.NewNop()
	cfg := &config.Config{
		CacheMaxSizeMB:   10,
		CacheNumCounters: 1000,
	}
	cacheStore, err := cache.New(cfg, logger)
	if err != nil {
		t.Fatalf("failed to create cache: %v", err)
	}
	defer cacheStore.Close()

	adminKey := "test-master-admin-key-12345"
	mw := ProxyAuth(cacheStore, adminKey)

	r := gin.New()
	r.Use(mw)
	r.GET("/v1/models", func(c *gin.Context) {
		tenantID, _ := c.Get("tenant_id")
		tenantName, _ := c.Get("tenant_name")
		c.JSON(http.StatusOK, gin.H{
			"status":      "ok",
			"tenant_id":   tenantID,
			"tenant_name": tenantName,
		})
	})

	// 1. Missing Authorization header -> 401
	req := httptest.NewRequest("GET", "/v1/models", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for missing auth, got %d", w.Code)
	}

	// 2. Admin key -> 200 OK with tenant_id "admin"
	req = httptest.NewRequest("GET", "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer "+adminKey)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for master admin key, got %d", w.Code)
	}

	// 3. Invalid key -> 401
	req = httptest.NewRequest("GET", "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer invalid-key-xyz")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for invalid key, got %d", w.Code)
	}

	// 4. Valid tenant key in cache -> 200 OK
	cacheStore.Set(cache.TenantKey("tenant-key-abc"), &database.TenantRow{
		ID:           "tenant-uuid-1",
		Name:         "Acme Corp",
		IsActive:     true,
		RateLimitRPM: 60,
		TokenBalance: 5000,
	}, 100)
	cacheStore.Wait()

	req = httptest.NewRequest("GET", "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer tenant-key-abc")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for valid tenant key, got %d", w.Code)
	}
}
