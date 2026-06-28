package router

import (
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	"github.com/skadraneshghn/clever-ai-gate/api/admin"
	"github.com/skadraneshghn/clever-ai-gate/internal/cache"
	"github.com/skadraneshghn/clever-ai-gate/internal/config"
	"github.com/skadraneshghn/clever-ai-gate/internal/credentials"
	"github.com/skadraneshghn/clever-ai-gate/internal/health"
	"github.com/skadraneshghn/clever-ai-gate/internal/middleware"
	"github.com/skadraneshghn/clever-ai-gate/internal/proxy"
	"go.uber.org/zap"
)

// Dependencies holds all the shared dependencies for route handlers.
type Dependencies struct {
	Config     *config.Config
	DB         *pgxpool.Pool
	Cache      *cache.Store
	Vault      *credentials.Vault
	Logger     *zap.Logger
	Health     *health.Handler
	Proxy      *proxy.Handler
}

// NewEngine creates and configures the Gin engine with all routes.
//
// Route architecture:
//   - Health routes: no middleware (must always respond)
//   - Proxy routes: auth + rate limiting only (zero overhead)
//   - Admin routes: auth + recovery + CORS (full middleware stack)
//   - Swagger: no auth (documentation is public)
func NewEngine(deps *Dependencies) *gin.Engine {
	// Configure Gin for production
	gin.SetMode(deps.Config.GinMode)

	// Create engine WITHOUT default middleware
	// Default middleware adds logging and recovery which we don't want on the hot-path
	engine := gin.New()

	// --- Health routes (no middleware) ---
	engine.GET("/health", deps.Health.Liveness)
	engine.GET("/ready", deps.Health.Readiness)

	// --- Swagger UI (no auth) ---
	engine.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	// --- Proxy routes (minimal middleware for maximum throughput) ---
	proxyGroup := engine.Group("")
	{
		proxyGroup.Use(middleware.ProxyAuth(deps.Cache))
		rateLimiter := middleware.NewRateLimiter(deps.Config.DefaultRateLimitRPM)
		proxyGroup.Use(rateLimiter.Middleware())

		// Single catch-all for all OpenAI-compatible endpoints:
		// /v1/chat/completions, /v1/embeddings, /v1/images/generations,
		// /v1/audio/transcriptions, /v1/audio/speech, etc.
		// All routes use the same proxy handler — model-based routing
		// is determined by the "model" field in the JSON body, not the URL path.
		proxyGroup.POST("/v1/*proxyPath", deps.Proxy.Handle)
	}

	// --- Admin API routes (full middleware stack) ---
	adminGroup := engine.Group("/api/v1/admin")
	{
		adminGroup.Use(middleware.CORS())
		adminGroup.Use(middleware.Recovery(deps.Logger))
		adminGroup.Use(middleware.RequestID())
		adminGroup.Use(middleware.AdminAuth(deps.Config.AdminAPIKey))

		// Tenant management
		tenantHandler := admin.NewTenantHandler(deps.DB)
		adminGroup.GET("/tenants", tenantHandler.List)
		adminGroup.POST("/tenants", tenantHandler.Create)
		adminGroup.GET("/tenants/:id", tenantHandler.Get)
		adminGroup.PUT("/tenants/:id", tenantHandler.Update)
		adminGroup.DELETE("/tenants/:id", tenantHandler.Delete)

		// Model pool management
		poolHandler := admin.NewPoolHandler(deps.DB)
		adminGroup.GET("/pools", poolHandler.List)
		adminGroup.POST("/pools", poolHandler.Create)
		adminGroup.GET("/pools/:id", poolHandler.Get)
		adminGroup.PUT("/pools/:id", poolHandler.Update)
		adminGroup.DELETE("/pools/:id", poolHandler.Delete)

		// Credential management
		credHandler := admin.NewCredentialHandler(deps.DB, deps.Vault)
		adminGroup.POST("/credentials", credHandler.Create)
		adminGroup.GET("/credentials/:id", credHandler.Get)
		adminGroup.PUT("/credentials/:id", credHandler.Update)
		adminGroup.DELETE("/credentials/:id", credHandler.Delete)
	}

	return engine
}
