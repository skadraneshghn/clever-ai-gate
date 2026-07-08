package health

import (
	"net/http"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Handler provides health and readiness check endpoints.
type Handler struct {
	db        *pgxpool.Pool
	startTime time.Time
	ready     atomic.Bool
}

// New creates a new health handler.
func New(db *pgxpool.Pool) *Handler {
	return &Handler{
		db:        db,
		startTime: time.Now(),
	}
}

// SetReady marks the application as ready to receive traffic.
func (h *Handler) SetReady(ready bool) {
	h.ready.Store(ready)
}

// LivenessResponse represents the liveness probe response body.
type LivenessResponse struct {
	Status string `json:"status"`
	Uptime string `json:"uptime"`
}

// ReadinessResponse represents the readiness probe response body.
type ReadinessResponse struct {
	Status   string            `json:"status"`
	Checks   map[string]string `json:"checks"`
	Ready    bool              `json:"ready"`
}

// Liveness returns 200 if the process is alive.
// @Summary      Liveness probe
// @Description  Returns 200 if the server process is running
// @Tags         Health
// @Produce      json
// @Success      200  {object}  LivenessResponse
// @Router       /health [get]
func (h *Handler) Liveness(c *gin.Context) {
	c.JSON(http.StatusOK, LivenessResponse{
		Status: "alive",
		Uptime: time.Since(h.startTime).Round(time.Second).String(),
	})
}

// Readiness returns 200 only when all subsystems are operational.
// @Summary      Readiness probe
// @Description  Checks database connectivity and cache initialization
// @Tags         Health
// @Produce      json
// @Success      200  {object}  ReadinessResponse
// @Failure      503  {object}  ReadinessResponse
// @Router       /ready [get]
func (h *Handler) Readiness(c *gin.Context) {
	checks := make(map[string]string)
	allOK := true

	// Check database connectivity
	if h.db != nil {
		if err := h.db.Ping(c.Request.Context()); err != nil {
			checks["database"] = "unhealthy: " + err.Error()
			allOK = false
		} else {
			checks["database"] = "healthy"
		}
	} else {
		checks["database"] = "not configured"
	}

	// Check application readiness flag
	if !h.ready.Load() {
		checks["application"] = "not ready"
		allOK = false
	} else {
		checks["application"] = "ready"
	}

	status := http.StatusOK
	statusText := "ready"
	if !allOK {
		status = http.StatusServiceUnavailable
		statusText = "not ready"
	}

	c.JSON(status, ReadinessResponse{
		Status: statusText,
		Checks: checks,
		Ready:  allOK,
	})
}
