package router

import (
	"io/fs"
	"net/http"

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
	"github.com/skadraneshghn/clever-ai-gate/internal/playground"
	"github.com/skadraneshghn/clever-ai-gate/internal/proxy"
	"github.com/skadraneshghn/clever-ai-gate/internal/telemetry"
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
	LogHub     *telemetry.LogHub // non-blocking log broadcaster for the admin log viewer
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

	// --- Developer Playground (protected by HTTP Basic Auth) ---
	subFS, err := fs.Sub(playground.DistFS, "dist")
	if err != nil {
		panic("failed to read embedded playground dist: " + err.Error())
	}
	assetsFS, err := fs.Sub(playground.DistFS, "dist/assets")
	if err != nil {
		panic("failed to read embedded playground assets: " + err.Error())
	}

	// Read index.html at startup to serve it directly
	// This avoids http.FileServer's built-in redirect logic for "index.html" (which redirects to ./ and causes 404/redirection loops)
	indexContent, err := fs.ReadFile(subFS, "index.html")
	if err != nil {
		panic("failed to read embedded playground index.html: " + err.Error())
	}

	// All playground routes are protected by HTTP Basic Auth.
	// This secures both the SPA HTML and the compiled JS/CSS bundles.
	playgroundGroup := engine.Group("", middleware.PlaygroundBasicAuth(deps.Config.PlaygroundUser, deps.Config.PlaygroundPass))
	{
		// Serve Svelte client compiled CSS/JS from /assets/*
		playgroundGroup.StaticFS("/assets", http.FS(assetsFS))

		// Serve Svelte index.html for requests to /playground and /playground/*any
		// This explicitly serves the client on both paths to avoid Gin's automatic trailing slash
		// HTTP redirects which fail behind Clever Cloud's reverse proxy, while also resolving SPA 404s.
		playgroundGroup.GET("/playground", func(c *gin.Context) {
			c.Data(200, "text/html; charset=utf-8", indexContent)
		})
		playgroundGroup.GET("/playground/*any", func(c *gin.Context) {
			c.Data(200, "text/html; charset=utf-8", indexContent)
		})
	}

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
		proxyGroup.GET("/v1/models", deps.Proxy.ListModels)

		// Playground Chats CRUD (Tenant-isolated, requires tenant API Key)
		chatHandler := admin.NewChatHandler(deps.DB)
		proxyGroup.GET("/api/v1/playground/tenant", chatHandler.GetTenantInfo)
		proxyGroup.GET("/api/v1/playground/chats", chatHandler.List)
		proxyGroup.GET("/api/v1/playground/chats/:id", chatHandler.Get)
		proxyGroup.POST("/api/v1/playground/chats", chatHandler.Create)
		proxyGroup.PUT("/api/v1/playground/chats/:id", chatHandler.Update)
		proxyGroup.DELETE("/api/v1/playground/chats/:id", chatHandler.Delete)
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
		adminGroup.GET("/credentials", credHandler.List)
		adminGroup.POST("/credentials", credHandler.Create)
		adminGroup.GET("/credentials/:id", credHandler.Get)
		adminGroup.PUT("/credentials/:id", credHandler.Update)
		adminGroup.DELETE("/credentials/:id", credHandler.Delete)

		// Provider auto-discovery (registers all models for a given provider key)
		adminGroup.POST("/providers/nvidia", credHandler.RegisterNvidiaProvider)
		adminGroup.POST("/providers/ollama", credHandler.RegisterOllamaProvider)

		// Live log streaming and daily log file download
		if deps.LogHub != nil {
			logCtrl := admin.NewLogAdminController(deps.LogHub)
			adminGroup.GET("/logs/stream", logCtrl.StreamLiveCoreLogs)
			adminGroup.GET("/logs/download", logCtrl.DownloadTodayLogFile)
		}
	}

	return engine
}
