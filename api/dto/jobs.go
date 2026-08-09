package dto

import "github.com/skadraneshghn/clever-ai-gate/internal/jobs"

// --- Job Request DTOs ---

// CreateJobRequest is the payload for POST /api/v1/admin/jobs.
type CreateJobRequest struct {
	Name              string         `json:"name" binding:"required,min=1,max=255"`
	Description       string         `json:"description"`
	JobType           string         `json:"job_type" binding:"required"`
	ScheduleType      string         `json:"schedule_type" binding:"required,oneof=cron interval one_time manual"`
	CronExpression    string         `json:"cron_expression"`
	IntervalSeconds   int            `json:"interval_seconds"`
	RunAt             *string        `json:"run_at"`
	Payload           map[string]any `json:"payload"`
	Timezone          string         `json:"timezone"`
	MaxRetries        int            `json:"max_retries"`
	RetryDelaySeconds int            `json:"retry_delay_seconds"`
	TimeoutSeconds    int            `json:"timeout_seconds"`
	IsEnabled         *bool          `json:"is_enabled"`
	IsSingleton       *bool          `json:"is_singleton"`
	Tags              []string       `json:"tags"`
}

// UpdateJobRequest is the payload for PUT /api/v1/admin/jobs/:id.
type UpdateJobRequest struct {
	Name              *string        `json:"name"`
	Description       *string        `json:"description"`
	CronExpression    *string        `json:"cron_expression"`
	IntervalSeconds   *int           `json:"interval_seconds"`
	RunAt             *string        `json:"run_at"`
	Payload           map[string]any `json:"payload"`
	Timezone          *string        `json:"timezone"`
	MaxRetries        *int           `json:"max_retries"`
	RetryDelaySeconds *int           `json:"retry_delay_seconds"`
	TimeoutSeconds    *int           `json:"timeout_seconds"`
	IsEnabled         *bool          `json:"is_enabled"`
	IsSingleton       *bool          `json:"is_singleton"`
	Tags              []string       `json:"tags"`
}

// UpdateSettingsRequest is the payload for PUT /api/v1/admin/scheduler/settings.
type UpdateSettingsRequest struct {
	Settings jobs.SchedulerSettings `json:"settings" binding:"required"`
}

// --- Job Response DTOs ---

// JobListResponse is the paginated response for listing jobs.
type JobListResponse struct {
	Data   []*jobs.Job `json:"data"`
	Total  int64       `json:"total"`
	Limit  int         `json:"limit"`
	Offset int         `json:"offset"`
}

// JobRunListResponse is the paginated response for listing job runs.
type JobRunListResponse struct {
	Data   []*jobs.JobRun `json:"data"`
	Total  int64          `json:"total"`
	Limit  int            `json:"limit"`
	Offset int            `json:"offset"`
}

// TriggerResponse is returned when a job is manually triggered.
type TriggerResponse struct {
	RunID   string `json:"run_id"`
	Message string `json:"message"`
}

// SchedulerStatsResponse contains live scheduler statistics.
type SchedulerStatsResponse struct {
	TotalJobs           int64    `json:"total_jobs"`
	EnabledJobs         int64    `json:"enabled_jobs"`
	Running24h          int64    `json:"running_24h"`
	Pending24h          int64    `json:"pending_24h"`
	Completed24h        int64    `json:"completed_24h"`
	Failed24h           int64    `json:"failed_24h"`
	SchedulerPaused     bool     `json:"scheduler_paused"`
	RedisEnabled        bool     `json:"redis_enabled"`
	QueueDepth          int64    `json:"queue_depth"`
	DLQDepth            int64    `json:"dlq_depth"`
	RegisteredJobTypes  []string `json:"registered_job_types"`
	MaxConcurrentJobs   int      `json:"max_concurrent_jobs"`
	WorkerPoolSize      int      `json:"worker_pool_size"`
}
