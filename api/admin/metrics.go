package admin

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
)

// MetricsController provides analytics and metrics endpoints for the gateway dashboard.
type MetricsController struct {
	db        *pgxpool.Pool
	startTime time.Time
}

// NewMetricsController creates a new metrics controller.
func NewMetricsController(db *pgxpool.Pool) *MetricsController {
	return &MetricsController{
		db:        db,
		startTime: time.Now(),
	}
}

// DailyRequestStat represents logs aggregated by day.
type DailyRequestStat struct {
	Date       string `json:"date"`
	Total      int64  `json:"total"`
	Successful int64  `json:"successful"`
}

// ModelStat represents request count and average latency per model.
type ModelStat struct {
	Model      string  `json:"model"`
	Requests   int64   `json:"requests"`
	AvgLatency float64 `json:"avg_latency_ms"`
}

// TenantStat represents usage counts per tenant.
type TenantStat struct {
	Name        string `json:"name"`
	Requests    int64  `json:"requests"`
	TotalTokens int64  `json:"total_tokens"`
}

// DashboardStats holds aggregates and telemetry summaries for rendering the dashboard UI.
type DashboardStats struct {
	UptimeSeconds    int64              `json:"uptime_seconds"`
	TotalRequests    int64              `json:"total_requests"`
	SuccessfulReqs   int64              `json:"successful_requests"`
	SuccessRate      float64            `json:"success_rate"`
	AvgLatencyMs     float64            `json:"avg_latency_ms"`
	TotalTokens      int64              `json:"total_tokens"`
	PromptTokens     int64              `json:"prompt_tokens"`
	CompletionTokens int64              `json:"completion_tokens"`
	ActiveTenants    int64              `json:"active_tenants"`
	TotalPools       int64              `json:"total_pools"`
	TotalCredentials int64              `json:"total_credentials"`
	HealthyCreds     int64              `json:"healthy_credentials"`
	DailyStats       []DailyRequestStat `json:"daily_stats"`
	TopModels        []ModelStat        `json:"top_models"`
	TopTenants       []TenantStat       `json:"top_tenants"`
}

// GetDashboardStats queries aggregates and stats for rendering on the front-end dashboard.
// @Summary      Get dashboard statistics
// @Description  Queries and returns engine uptime, total logs count, token summaries, active configuration figures, and timeseries stats
// @Tags         Analytics
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  DashboardStats
// @Failure      500  {object}  dto.ErrorResponse
// @Router       /api/v1/admin/metrics [get]
func (ctrl *MetricsController) GetDashboardStats(c *gin.Context) {
	ctx := c.Request.Context()

	var stats DashboardStats
	stats.UptimeSeconds = int64(time.Since(ctrl.startTime).Seconds())

	// Initialize slices to avoid returning null in JSON responses
	stats.DailyStats = []DailyRequestStat{}
	stats.TopModels = []ModelStat{}
	stats.TopTenants = []TenantStat{}

	// 1. Core aggregates from request_logs
	err := ctrl.db.QueryRow(ctx, `
		SELECT 
			COUNT(*),
			COALESCE(SUM(CASE WHEN status_code >= 200 AND status_code < 400 THEN 1 ELSE 0 END), 0),
			COALESCE(AVG(latency_ms), 0),
			COALESCE(SUM(prompt_tokens + completion_tokens), 0),
			COALESCE(SUM(prompt_tokens), 0),
			COALESCE(SUM(completion_tokens), 0)
		FROM request_logs
	`).Scan(
		&stats.TotalRequests,
		&stats.SuccessfulReqs,
		&stats.AvgLatencyMs,
		&stats.TotalTokens,
		&stats.PromptTokens,
		&stats.CompletionTokens,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to query aggregated log stats", "details": err.Error()})
		return
	}

	if stats.TotalRequests > 0 {
		stats.SuccessRate = (float64(stats.SuccessfulReqs) / float64(stats.TotalRequests)) * 100
	} else {
		stats.SuccessRate = 100
	}

	// 2. Counts from other configuration tables
	_ = ctrl.db.QueryRow(ctx, "SELECT COUNT(*) FROM tenants WHERE is_active = true").Scan(&stats.ActiveTenants)
	_ = ctrl.db.QueryRow(ctx, "SELECT COUNT(*) FROM model_pools").Scan(&stats.TotalPools)
	_ = ctrl.db.QueryRow(ctx, `
		SELECT 
			COUNT(*),
			COALESCE(SUM(CASE WHEN is_healthy = true THEN 1 ELSE 0 END), 0)
		FROM credentials
	`).Scan(&stats.TotalCredentials, &stats.HealthyCreds)

	// 3. Daily request counts (past 7 days)
	rows, err := ctrl.db.Query(ctx, `
		SELECT 
			TO_CHAR(created_at, 'YYYY-MM-DD') as day,
			COUNT(*),
			COALESCE(SUM(CASE WHEN status_code >= 200 AND status_code < 400 THEN 1 ELSE 0 END), 0)
		FROM request_logs
		WHERE created_at >= NOW() - INTERVAL '7 days'
		GROUP BY day
		ORDER BY day ASC
	`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var d DailyRequestStat
			if err := rows.Scan(&d.Date, &d.Total, &d.Successful); err == nil {
				stats.DailyStats = append(stats.DailyStats, d)
			}
		}
	}

	// 4. Top models
	rowsModels, err := ctrl.db.Query(ctx, `
		SELECT 
			COALESCE(model, 'unknown'),
			COUNT(*),
			COALESCE(AVG(latency_ms), 0)
		FROM request_logs
		GROUP BY model
		ORDER BY COUNT(*) DESC
		LIMIT 5
	`)
	if err == nil {
		defer rowsModels.Close()
		for rowsModels.Next() {
			var m ModelStat
			if err := rowsModels.Scan(&m.Model, &m.Requests, &m.AvgLatency); err == nil {
				stats.TopModels = append(stats.TopModels, m)
			}
		}
	}

	// 5. Top tenants
	rowsTenants, err := ctrl.db.Query(ctx, `
		SELECT 
			COALESCE(t.name, 'System / Direct'),
			COUNT(*),
			COALESCE(SUM(prompt_tokens + completion_tokens), 0)
		FROM request_logs r
		LEFT JOIN tenants t ON r.tenant_id = t.id
		GROUP BY t.name
		ORDER BY COUNT(*) DESC
		LIMIT 5
	`)
	if err == nil {
		defer rowsTenants.Close()
		for rowsTenants.Next() {
			var t TenantStat
			if err := rowsTenants.Scan(&t.Name, &t.Requests, &t.TotalTokens); err == nil {
				stats.TopTenants = append(stats.TopTenants, t)
			}
		}
	}

	c.JSON(http.StatusOK, stats)
}
