package admin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/skadraneshghn/clever-ai-gate/api/dto"
	"github.com/skadraneshghn/clever-ai-gate/internal/jobs"
	"go.uber.org/zap"
)

// JobHandler provides full CRUD and management for the job scheduling system.
type JobHandler struct {
	db        *pgxpool.Pool
	scheduler *jobs.Scheduler
	logger    *zap.Logger
}

// NewJobHandler creates a new job handler.
func NewJobHandler(db *pgxpool.Pool, scheduler *jobs.Scheduler, logger *zap.Logger) *JobHandler {
	return &JobHandler{db: db, scheduler: scheduler, logger: logger}
}

// ListJobs returns all jobs with pagination and optional filtering.
//
// @Summary      List scheduled jobs
// @Description  Returns all jobs with optional filtering by status, type, or search term.
// @Tags         Jobs
// @Produce      json
// @Security     BearerAuth
// @Param        limit      query  int     false  "Page size"           example(20)
// @Param        offset     query  int     false  "Page offset"         example(0)
// @Param        enabled    query  bool    false  "Filter by enabled"
// @Param        job_type   query  string  false  "Filter by job type"
// @Param        search     query  string  false  "Search in name/description"
// @Success      200  {object}  dto.JobListResponse
// @Failure      500  {object}  dto.ErrorResponse
// @Router       /api/v1/admin/jobs [get]
func (h *JobHandler) ListJobs(c *gin.Context) {
	limit := 50
	offset := 0
	if l := c.Query("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			limit = n
		}
	}
	if o := c.Query("offset"); o != "" {
		if n, err := strconv.Atoi(o); err == nil && n >= 0 {
			offset = n
		}
	}

	where := []string{}
	args := []any{}
	argN := 1

	if enabled := c.Query("enabled"); enabled != "" {
		where = append(where, fmt.Sprintf("is_enabled = $%d", argN))
		args = append(args, enabled == "true")
		argN++
	}
	if jobType := c.Query("job_type"); jobType != "" {
		where = append(where, fmt.Sprintf("job_type = $%d", argN))
		args = append(args, jobType)
		argN++
	}
	if search := c.Query("search"); search != "" {
		where = append(where, fmt.Sprintf("(name ILIKE $%d OR description ILIKE $%d OR job_type ILIKE $%d)", argN, argN, argN))
		args = append(args, "%"+search+"%")
		argN++
	}

	whereClause := ""
	if len(where) > 0 {
		whereClause = "WHERE " + strings.Join(where, " AND ")
	}

	// Count
	var total int64
	countArgs := append([]any{}, args...)
	if err := h.db.QueryRow(c.Request.Context(),
		fmt.Sprintf("SELECT COUNT(*) FROM jobs %s", whereClause), countArgs...,
	).Scan(&total); err != nil {
		c.JSON(http.StatusInternalServerError, dto.ErrorResponse{Error: "count query failed", Details: err.Error()})
		return
	}

	// Fetch
	queryArgs := append(args, limit, offset)
	rows, err := h.db.Query(c.Request.Context(), fmt.Sprintf(`
		SELECT id, name, description, job_type, schedule_type,
		       COALESCE(cron_expression, ''), COALESCE(interval_seconds, 0),
		       run_at, payload, timezone, max_retries, retry_delay_seconds,
		       timeout_seconds, is_enabled, is_singleton,
		       COALESCE(tags, '{}'), last_run_at, next_run_at,
		       COALESCE(last_run_status, ''), run_count, success_count, failure_count,
		       created_at::text, updated_at::text
		FROM jobs %s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d
	`, whereClause, argN, argN+1), queryArgs...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, dto.ErrorResponse{Error: "query failed", Details: err.Error()})
		return
	}
	defer rows.Close()

	var result []*jobs.Job
	for rows.Next() {
		j := &jobs.Job{}
		var payloadJSON []byte
		if err := rows.Scan(
			&j.ID, &j.Name, &j.Description, &j.JobType, &j.ScheduleType,
			&j.CronExpression, &j.IntervalSeconds,
			&j.RunAt, &payloadJSON, &j.Timezone, &j.MaxRetries, &j.RetryDelaySeconds,
			&j.TimeoutSeconds, &j.IsEnabled, &j.IsSingleton,
			&j.Tags, &j.LastRunAt, &j.NextRunAt,
			&j.LastRunStatus, &j.RunCount, &j.SuccessCount, &j.FailureCount,
			&j.CreatedAt, &j.UpdatedAt,
		); err != nil {
			continue
		}
		if payloadJSON != nil {
			_ = json.Unmarshal(payloadJSON, &j.Payload)
		}
		if j.Payload == nil {
			j.Payload = make(map[string]any)
		}
		result = append(result, j)
	}

	c.JSON(http.StatusOK, dto.JobListResponse{
		Data:   result,
		Total:  total,
		Limit:  limit,
		Offset: offset,
	})
}

// GetJob returns a single job by ID.
//
// @Summary      Get a job
// @Tags         Jobs
// @Produce      json
// @Security     BearerAuth
// @Param        id  path  string  true  "Job ID"
// @Success      200  {object}  jobs.Job
// @Failure      404  {object}  dto.ErrorResponse
// @Router       /api/v1/admin/jobs/{id} [get]
func (h *JobHandler) GetJob(c *gin.Context) {
	id := c.Param("id")
	j, err := h.fetchJob(c, id)
	if err != nil {
		c.JSON(http.StatusNotFound, dto.ErrorResponse{Error: "job not found", Details: err.Error()})
		return
	}
	c.JSON(http.StatusOK, j)
}

// CreateJob creates a new scheduled job.
//
// @Summary      Create a job
// @Tags         Jobs
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body  body  dto.CreateJobRequest  true  "Job definition"
// @Success      201  {object}  jobs.Job
// @Failure      400  {object}  dto.ErrorResponse
// @Router       /api/v1/admin/jobs [post]
func (h *JobHandler) CreateJob(c *gin.Context) {
	var req dto.CreateJobRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: "invalid request", Details: err.Error()})
		return
	}

	// Validate job type
	if !h.scheduler.Registry().Has(req.JobType) {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{
			Error:   "unknown job type",
			Details: fmt.Sprintf("job type %q is not registered; available: %v", req.JobType, h.scheduler.Registry().ListTypes()),
		})
		return
	}

	// Defaults
	if req.MaxRetries == 0 {
		req.MaxRetries = h.scheduler.Config().MaxRetries
	}
	if req.RetryDelaySeconds == 0 {
		req.RetryDelaySeconds = h.scheduler.Config().RetryDelay
	}
	if req.TimeoutSeconds == 0 {
		req.TimeoutSeconds = h.scheduler.Config().JobTimeout
	}
	if req.Timezone == "" {
		req.Timezone = h.scheduler.Config().Timezone
	}

	enabled := true
	if req.IsEnabled != nil {
		enabled = *req.IsEnabled
	}
	singleton := h.scheduler.Config().SingletonMode
	if req.IsSingleton != nil {
		singleton = *req.IsSingleton
	}

	payloadJSON, _ := json.Marshal(req.Payload)
	if req.Payload == nil {
		payloadJSON = []byte("{}")
	}

	var runAt *time.Time
	if req.RunAt != nil && *req.RunAt != "" {
		t, err := time.Parse(time.RFC3339, *req.RunAt)
		if err != nil {
			c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: "invalid run_at format", Details: "use RFC3339 format"})
			return
		}
		runAt = &t
	}

	var jobID string
	err := h.db.QueryRow(c.Request.Context(), `
		INSERT INTO jobs (
			name, description, job_type, schedule_type, cron_expression,
			interval_seconds, run_at, payload, timezone, max_retries,
			retry_delay_seconds, timeout_seconds, is_enabled, is_singleton, tags
		) VALUES ($1,$2,$3,$4,NULLIF($5,''),$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
		RETURNING id
	`, req.Name, req.Description, req.JobType, req.ScheduleType, req.CronExpression,
		nullableInt(req.IntervalSeconds), runAt, payloadJSON, req.Timezone,
		req.MaxRetries, req.RetryDelaySeconds, req.TimeoutSeconds,
		enabled, singleton, req.Tags,
	).Scan(&jobID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, dto.ErrorResponse{Error: "failed to create job", Details: err.Error()})
		return
	}

	j, _ := h.fetchJob(c, jobID)

	// Register with live scheduler if enabled
	if enabled && j != nil {
		if err := h.scheduler.RegisterJobInGocron(*j); err != nil {
			h.logger.Warn("job created but not scheduled (will run on next restart)",
				zap.String("job_id", jobID),
				zap.Error(err),
			)
		}
	}

	c.JSON(http.StatusCreated, j)
}

// UpdateJob updates an existing job.
//
// @Summary      Update a job
// @Tags         Jobs
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path  string              true  "Job ID"
// @Param        body  body  dto.UpdateJobRequest  true  "Fields to update"
// @Success      200  {object}  jobs.Job
// @Router       /api/v1/admin/jobs/{id} [put]
func (h *JobHandler) UpdateJob(c *gin.Context) {
	id := c.Param("id")
	var req dto.UpdateJobRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: "invalid request", Details: err.Error()})
		return
	}

	setClauses := []string{}
	args := []any{}
	argN := 1

	if req.Name != nil {
		setClauses = append(setClauses, fmt.Sprintf("name = $%d", argN))
		args = append(args, *req.Name)
		argN++
	}
	if req.Description != nil {
		setClauses = append(setClauses, fmt.Sprintf("description = $%d", argN))
		args = append(args, *req.Description)
		argN++
	}
	if req.CronExpression != nil {
		setClauses = append(setClauses, fmt.Sprintf("cron_expression = $%d", argN))
		args = append(args, *req.CronExpression)
		argN++
	}
	if req.IntervalSeconds != nil {
		setClauses = append(setClauses, fmt.Sprintf("interval_seconds = $%d", argN))
		args = append(args, *req.IntervalSeconds)
		argN++
	}
	if req.Timezone != nil {
		setClauses = append(setClauses, fmt.Sprintf("timezone = $%d", argN))
		args = append(args, *req.Timezone)
		argN++
	}
	if req.MaxRetries != nil {
		setClauses = append(setClauses, fmt.Sprintf("max_retries = $%d", argN))
		args = append(args, *req.MaxRetries)
		argN++
	}
	if req.RetryDelaySeconds != nil {
		setClauses = append(setClauses, fmt.Sprintf("retry_delay_seconds = $%d", argN))
		args = append(args, *req.RetryDelaySeconds)
		argN++
	}
	if req.TimeoutSeconds != nil {
		setClauses = append(setClauses, fmt.Sprintf("timeout_seconds = $%d", argN))
		args = append(args, *req.TimeoutSeconds)
		argN++
	}
	if req.IsEnabled != nil {
		setClauses = append(setClauses, fmt.Sprintf("is_enabled = $%d", argN))
		args = append(args, *req.IsEnabled)
		argN++
	}
	if req.IsSingleton != nil {
		setClauses = append(setClauses, fmt.Sprintf("is_singleton = $%d", argN))
		args = append(args, *req.IsSingleton)
		argN++
	}
	if req.Payload != nil {
		payloadJSON, _ := json.Marshal(req.Payload)
		setClauses = append(setClauses, fmt.Sprintf("payload = $%d", argN))
		args = append(args, payloadJSON)
		argN++
	}
	if req.Tags != nil {
		setClauses = append(setClauses, fmt.Sprintf("tags = $%d", argN))
		args = append(args, req.Tags)
		argN++
	}

	if len(setClauses) == 0 {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: "no fields to update"})
		return
	}

	args = append(args, id)
	query := fmt.Sprintf("UPDATE jobs SET %s, updated_at = NOW() WHERE id = $%d",
		strings.Join(setClauses, ", "), argN)

	result, err := h.db.Exec(c.Request.Context(), query, args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, dto.ErrorResponse{Error: "update failed", Details: err.Error()})
		return
	}
	if result.RowsAffected() == 0 {
		c.JSON(http.StatusNotFound, dto.ErrorResponse{Error: "job not found"})
		return
	}

	// Re-register in gocron (remove old, add new)
	_ = h.scheduler.UnregisterJobFromGocron(id)
	if j, err := h.fetchJob(c, id); err == nil && j.IsEnabled {
		_ = h.scheduler.RegisterJobInGocron(*j)
	}

	j, _ := h.fetchJob(c, id)
	c.JSON(http.StatusOK, j)
}

// DeleteJob deletes a job and its run history.
//
// @Summary      Delete a job
// @Tags         Jobs
// @Produce      json
// @Security     BearerAuth
// @Param        id  path  string  true  "Job ID"
// @Success      204
// @Router       /api/v1/admin/jobs/{id} [delete]
func (h *JobHandler) DeleteJob(c *gin.Context) {
	id := c.Param("id")

	_ = h.scheduler.UnregisterJobFromGocron(id)

	result, err := h.db.Exec(c.Request.Context(), `DELETE FROM jobs WHERE id = $1`, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, dto.ErrorResponse{Error: "delete failed", Details: err.Error()})
		return
	}
	if result.RowsAffected() == 0 {
		c.JSON(http.StatusNotFound, dto.ErrorResponse{Error: "job not found"})
		return
	}
	c.Status(http.StatusNoContent)
}

// TriggerJob manually triggers a job for immediate execution.
//
// @Summary      Trigger job now
// @Tags         Jobs
// @Produce      json
// @Security     BearerAuth
// @Param        id  path  string  true  "Job ID"
// @Success      202  {object}  dto.TriggerResponse
// @Router       /api/v1/admin/jobs/{id}/trigger [post]
func (h *JobHandler) TriggerJob(c *gin.Context) {
	id := c.Param("id")
	runID, err := h.scheduler.TriggerNow(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, dto.ErrorResponse{Error: "trigger failed", Details: err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, dto.TriggerResponse{
		RunID:   runID,
		Message: "Job triggered successfully",
	})
}

// PauseJob disables a job without deleting it.
//
// @Summary      Pause a job
// @Tags         Jobs
// @Produce      json
// @Security     BearerAuth
// @Param        id  path  string  true  "Job ID"
// @Success      200  {object}  jobs.Job
// @Router       /api/v1/admin/jobs/{id}/pause [post]
func (h *JobHandler) PauseJob(c *gin.Context) {
	id := c.Param("id")
	_, err := h.db.Exec(c.Request.Context(), `UPDATE jobs SET is_enabled = FALSE, updated_at = NOW() WHERE id = $1`, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, dto.ErrorResponse{Error: "pause failed", Details: err.Error()})
		return
	}
	_ = h.scheduler.UnregisterJobFromGocron(id)
	j, _ := h.fetchJob(c, id)
	c.JSON(http.StatusOK, j)
}

// ResumeJob re-enables a paused job.
//
// @Summary      Resume a job
// @Tags         Jobs
// @Produce      json
// @Security     BearerAuth
// @Param        id  path  string  true  "Job ID"
// @Success      200  {object}  jobs.Job
// @Router       /api/v1/admin/jobs/{id}/resume [post]
func (h *JobHandler) ResumeJob(c *gin.Context) {
	id := c.Param("id")
	_, err := h.db.Exec(c.Request.Context(), `UPDATE jobs SET is_enabled = TRUE, updated_at = NOW() WHERE id = $1`, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, dto.ErrorResponse{Error: "resume failed", Details: err.Error()})
		return
	}
	if j, err := h.fetchJob(c, id); err == nil {
		_ = h.scheduler.RegisterJobInGocron(*j)
		c.JSON(http.StatusOK, j)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "job resumed"})
}

// GetJobRuns returns run history for a specific job.
//
// @Summary      Get job run history
// @Tags         Jobs
// @Produce      json
// @Security     BearerAuth
// @Param        id      path   string  true   "Job ID"
// @Param        limit   query  int     false  "Page size"
// @Param        offset  query  int     false  "Page offset"
// @Success      200  {object}  dto.JobRunListResponse
// @Router       /api/v1/admin/jobs/{id}/runs [get]
func (h *JobHandler) GetJobRuns(c *gin.Context) {
	id := c.Param("id")
	limit, offset := parsePagination(c, 50)
	h.listRuns(c, &id, limit, offset)
}

// GetAllRuns returns the global run history across all jobs.
//
// @Summary      Get all job runs
// @Tags         Jobs
// @Produce      json
// @Security     BearerAuth
// @Param        limit   query  int     false  "Page size"
// @Param        offset  query  int     false  "Page offset"
// @Param        status  query  string  false  "Filter by status"
// @Success      200  {object}  dto.JobRunListResponse
// @Router       /api/v1/admin/jobs/runs [get]
func (h *JobHandler) GetAllRuns(c *gin.Context) {
	limit, offset := parsePagination(c, 100)
	h.listRuns(c, nil, limit, offset)
}

// DeleteRun deletes a specific run record.
//
// @Summary      Delete a job run record
// @Tags         Jobs
// @Produce      json
// @Security     BearerAuth
// @Param        run_id  path  string  true  "Run ID"
// @Success      204
// @Router       /api/v1/admin/jobs/runs/{run_id} [delete]
func (h *JobHandler) DeleteRun(c *gin.Context) {
	runID := c.Param("run_id")
	_, err := h.db.Exec(c.Request.Context(), `DELETE FROM job_runs WHERE id = $1`, runID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, dto.ErrorResponse{Error: "delete failed", Details: err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

// GetSchedulerSettings returns current scheduler settings with metadata.
//
// @Summary      Get scheduler settings
// @Tags         Scheduler
// @Produce      json
// @Security     BearerAuth
// @Success      200  {array}   jobs.SettingRow
// @Router       /api/v1/admin/scheduler/settings [get]
func (h *JobHandler) GetSchedulerSettings(c *gin.Context) {
	rows, err := h.scheduler.Settings().GetAllWithMeta(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, dto.ErrorResponse{Error: "failed to load settings", Details: err.Error()})
		return
	}
	c.JSON(http.StatusOK, rows)
}

// UpdateSchedulerSettings updates scheduler settings and triggers a live reload.
//
// @Summary      Update scheduler settings
// @Tags         Scheduler
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body  body  dto.UpdateSettingsRequest  true  "New settings"
// @Success      200  {object}  jobs.SchedulerSettings
// @Router       /api/v1/admin/scheduler/settings [put]
func (h *JobHandler) UpdateSchedulerSettings(c *gin.Context) {
	var req dto.UpdateSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: "invalid request", Details: err.Error()})
		return
	}
	if err := h.scheduler.Settings().SaveAll(c.Request.Context(), req.Settings); err != nil {
		c.JSON(http.StatusInternalServerError, dto.ErrorResponse{Error: "failed to save settings", Details: err.Error()})
		return
	}
	if err := h.scheduler.ReloadSettings(c.Request.Context()); err != nil {
		h.logger.Warn("settings saved but live reload failed", zap.Error(err))
	}
	c.JSON(http.StatusOK, req.Settings)
}

// GetSchedulerStats returns live runtime statistics.
//
// @Summary      Get scheduler stats
// @Tags         Scheduler
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  dto.SchedulerStatsResponse
// @Router       /api/v1/admin/scheduler/stats [get]
func (h *JobHandler) GetSchedulerStats(c *gin.Context) {
	statsMap, err := h.scheduler.Stats(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, dto.ErrorResponse{Error: "failed to get stats", Details: err.Error()})
		return
	}
	c.JSON(http.StatusOK, statsMap)
}

// GetRegisteredTypes returns all registered job executor types.
//
// @Summary      Get registered job types
// @Tags         Scheduler
// @Produce      json
// @Security     BearerAuth
// @Success      200  {array}  string
// @Router       /api/v1/admin/scheduler/types [get]
func (h *JobHandler) GetRegisteredTypes(c *gin.Context) {
	c.JSON(http.StatusOK, h.scheduler.Registry().ListTypes())
}

// RestartScheduler gracefully restarts the scheduler (for settings changes that require it).
//
// @Summary      Restart scheduler
// @Tags         Scheduler
// @Produce      json
// @Security     BearerAuth
// @Success      200
// @Router       /api/v1/admin/scheduler/restart [post]
func (h *JobHandler) RestartScheduler(c *gin.Context) {
	if err := h.scheduler.ReloadSettings(c.Request.Context()); err != nil {
		c.JSON(http.StatusInternalServerError, dto.ErrorResponse{Error: "reload failed", Details: err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Scheduler settings reloaded successfully"})
}

// --- helpers ---

func (h *JobHandler) fetchJob(c *gin.Context, id string) (*jobs.Job, error) {
	ctx := c.Request.Context()
	j := &jobs.Job{}
	var payloadJSON []byte
	var cronExpr *string
	var intervalSec *int
	var tags []string

	err := h.db.QueryRow(ctx, `
		SELECT id, name, description, job_type, schedule_type,
		       cron_expression, interval_seconds, run_at, payload,
		       timezone, max_retries, retry_delay_seconds, timeout_seconds,
		       is_enabled, is_singleton, COALESCE(tags, '{}'), last_run_at, next_run_at,
		       COALESCE(last_run_status, ''), run_count, success_count, failure_count,
		       created_at::text, updated_at::text
		FROM jobs WHERE id = $1
	`, id).Scan(
		&j.ID, &j.Name, &j.Description, &j.JobType, &j.ScheduleType,
		&cronExpr, &intervalSec, &j.RunAt, &payloadJSON,
		&j.Timezone, &j.MaxRetries, &j.RetryDelaySeconds, &j.TimeoutSeconds,
		&j.IsEnabled, &j.IsSingleton, &tags, &j.LastRunAt, &j.NextRunAt,
		&j.LastRunStatus, &j.RunCount, &j.SuccessCount, &j.FailureCount,
		&j.CreatedAt, &j.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	if cronExpr != nil {
		j.CronExpression = *cronExpr
	}
	if intervalSec != nil {
		j.IntervalSeconds = *intervalSec
	}
	j.Tags = tags
	if j.Tags == nil {
		j.Tags = []string{}
	}
	if payloadJSON != nil {
		_ = json.Unmarshal(payloadJSON, &j.Payload)
	}
	if j.Payload == nil {
		j.Payload = make(map[string]any)
	}
	return j, nil
}

func (h *JobHandler) listRuns(c *gin.Context, jobID *string, limit, offset int) {
	where := []string{}
	args := []any{}
	argN := 1

	if jobID != nil {
		where = append(where, fmt.Sprintf("jr.job_id = $%d", argN))
		args = append(args, *jobID)
		argN++
	}
	if status := c.Query("status"); status != "" {
		where = append(where, fmt.Sprintf("jr.status = $%d", argN))
		args = append(args, status)
		argN++
	}

	whereClause := ""
	if len(where) > 0 {
		whereClause = "WHERE " + strings.Join(where, " AND ")
	}

	var total int64
	countArgs := append([]any{}, args...)
	_ = h.db.QueryRow(c.Request.Context(),
		fmt.Sprintf("SELECT COUNT(*) FROM job_runs jr %s", whereClause), countArgs...,
	).Scan(&total)

	queryArgs := append(args, limit, offset)
	rows, err := h.db.Query(c.Request.Context(), fmt.Sprintf(`
		SELECT jr.id, jr.job_id, j.name, jr.status, jr.triggered_by, jr.attempt,
		       jr.started_at, jr.finished_at, jr.duration_ms,
		       COALESCE(jr.output, ''), COALESCE(jr.error_message, ''),
		       COALESCE(jr.host, ''), jr.created_at::text
		FROM job_runs jr
		LEFT JOIN jobs j ON jr.job_id = j.id
		%s
		ORDER BY jr.created_at DESC
		LIMIT $%d OFFSET $%d
	`, whereClause, argN, argN+1), queryArgs...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, dto.ErrorResponse{Error: "query failed", Details: err.Error()})
		return
	}
	defer rows.Close()

	var result []*jobs.JobRun
	for rows.Next() {
		r := &jobs.JobRun{}
		var jobName string
		if err := rows.Scan(
			&r.ID, &r.JobID, &jobName, &r.Status, &r.TriggeredBy, &r.Attempt,
			&r.StartedAt, &r.FinishedAt, &r.DurationMs,
			&r.Output, &r.ErrorMessage, &r.Host, &r.CreatedAt,
		); err != nil {
			continue
		}
		r.JobName = jobName
		result = append(result, r)
	}

	c.JSON(http.StatusOK, dto.JobRunListResponse{
		Data:   result,
		Total:  total,
		Limit:  limit,
		Offset: offset,
	})
}

func parsePagination(c *gin.Context, defaultLimit int) (int, int) {
	limit := defaultLimit
	offset := 0
	if l := c.Query("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			limit = n
		}
	}
	if o := c.Query("offset"); o != "" {
		if n, err := strconv.Atoi(o); err == nil && n >= 0 {
			offset = n
		}
	}
	return limit, offset
}

func nullableInt(v int) *int {
	if v == 0 {
		return nil
	}
	return &v
}
