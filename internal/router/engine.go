package router

import (
	"io/fs"
	"net/http"
	"strings"

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
	"github.com/skadraneshghn/clever-ai-gate/internal/redisclient"
	"github.com/skadraneshghn/clever-ai-gate/internal/telemetry"
	"go.uber.org/zap"
)

// Dependencies holds all the shared dependencies for route handlers.
type Dependencies struct {
	Config      *config.Config
	DB          *pgxpool.Pool
	Cache       *cache.Store
	TenantCache *cache.RedisTenantCache // nil when Redis not configured
	Redis       *redisclient.Client     // nil when Redis not configured
	Vault       *credentials.Vault
	Logger      *zap.Logger
	Health      *health.Handler
	Proxy       *proxy.Handler
	LogHub      *telemetry.LogHub // non-blocking log broadcaster for the admin log viewer
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
		// Serve Svelte client static assets from the subFS using http.FileServer
		fileServer := http.StripPrefix("/playground", http.FileServer(http.FS(subFS)))

		// Serve Svelte index.html for requests to /playground
		playgroundGroup.GET("/playground", func(c *gin.Context) {
			c.Data(200, "text/html; charset=utf-8", indexContent)
		})

		// Serve Svelte index.html or dynamic static assets for /playground/*any
		// SPA routing strategy: only serve real files if the path has a file extension
		// (e.g. .js, .css, .png, .json). All other paths are client-side routes → index.html
		playgroundGroup.GET("/playground/*any", func(c *gin.Context) {
			path := c.Param("any")
			if path == "" || path == "/" {
				c.Data(200, "text/html; charset=utf-8", indexContent)
				return
			}

			// If the path contains a dot after the last slash, it's a real static asset
			// (e.g. /playground/_app/immutable/chunks/foo.js, /playground/favicon.png)
			// Otherwise it's a SPA route like /playground/logs → serve index.html
			lastSegment := path[strings.LastIndex(path, "/")+1:]
			if strings.Contains(lastSegment, ".") {
				// Real static asset — let the file server handle it
				fileServer.ServeHTTP(c.Writer, c.Request)
				return
			}

			// SPA client-side route — always serve index.html
			c.Data(200, "text/html; charset=utf-8", indexContent)
		})

		// Config endpoint to return admin key and default tenant key to Basic Auth logged-in user
		playgroundGroup.GET("/api/v1/playground/config", func(c *gin.Context) {
			var tenantKey string
			err := deps.DB.QueryRow(c.Request.Context(), `
				SELECT api_key FROM tenants WHERE is_active = true ORDER BY created_at LIMIT 1
			`).Scan(&tenantKey)
			if err != nil {
				// No active tenant exists, let's create a default one
				tenantKey = "cag_default_tenant_key_salman_136517"
				_, err = deps.DB.Exec(c.Request.Context(), `
					INSERT INTO tenants (name, api_key, token_balance, rate_limit_rpm)
					VALUES ($1, $2, $3, $4)
					ON CONFLICT (api_key) DO NOTHING
				`, "Default Tenant", tenantKey, 1000000000, 120)
				if err != nil {
					c.JSON(500, gin.H{"error": "failed to seed default tenant", "details": err.Error()})
					return
				}
			}

			c.JSON(200, gin.H{
				"admin_key":  deps.Config.AdminAPIKey,
				"tenant_key": tenantKey,
			})
		})
	}

	// --- Proxy routes (minimal middleware for maximum throughput) ---
	proxyGroup := engine.Group("")
	{
		// Auth: prefer two-layer Ristretto+Redis cache when available
		if deps.TenantCache != nil {
			proxyGroup.Use(middleware.ProxyAuthWithRedis(deps.TenantCache))
		} else {
			proxyGroup.Use(middleware.ProxyAuth(deps.Cache))
		}

		// Rate limiting: prefer Redis Lua sliding-window when available
		if deps.Redis != nil && deps.Redis.Unwrap() != nil {
			redisRL := middleware.NewRedisRateLimiter(deps.Redis.Unwrap(), deps.Config.DefaultRateLimitRPM)
			proxyGroup.Use(redisRL.Middleware())
		} else {
			rateLimiter := middleware.NewRateLimiter(deps.Config.DefaultRateLimitRPM)
			proxyGroup.Use(rateLimiter.Middleware())
		}

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
		poolHandler := admin.NewPoolHandler(deps.DB, deps.Vault)
		adminGroup.GET("/pools", poolHandler.List)
		adminGroup.POST("/pools", poolHandler.Create)
		adminGroup.GET("/pools/:id", poolHandler.Get)
		adminGroup.PUT("/pools/:id", poolHandler.Update)
		adminGroup.DELETE("/pools/:id", poolHandler.Delete)
		adminGroup.GET("/pools/:id/logs", poolHandler.GetLogs)
		adminGroup.POST("/pools/:id/credentials/:cred_id/test", poolHandler.TestCredential)

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
		adminGroup.POST("/providers/custom", credHandler.RegisterCustomProvider)
		adminGroup.POST("/providers/openrouter", credHandler.RegisterOpenRouterProvider)

		// Live log streaming and daily log file download
		if deps.LogHub != nil {
			logCtrl := admin.NewLogAdminController(deps.LogHub)
			adminGroup.GET("/logs/stream", logCtrl.StreamLiveCoreLogs)
			adminGroup.GET("/logs/download", logCtrl.DownloadTodayLogFile)
		}

		// Dashboard statistics & metrics
		metricsCtrl := admin.NewMetricsController(deps.DB)
		adminGroup.GET("/metrics", metricsCtrl.GetDashboardStats)
	}

	return engine
}
