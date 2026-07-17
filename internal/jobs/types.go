// Package jobs provides a full-featured job scheduling system for Clever AI Gate.
// It wraps gocron v2 as the scheduling backbone, Redis for distributed coordination
// and async queuing, and PostgreSQL for persistent job definitions and run history.
package jobs

import (
	"context"
	"time"
)

// JobStatus represents the lifecycle state of a job definition.
type JobStatus string

const (
	JobStatusEnabled  JobStatus = "enabled"
	JobStatusDisabled JobStatus = "disabled"
	JobStatusPaused   JobStatus = "paused"
)

// RunStatus represents the state of a single job execution.
type RunStatus string

const (
	RunStatusPending   RunStatus = "pending"
	RunStatusRunning   RunStatus = "running"
	RunStatusSuccess   RunStatus = "success"
	RunStatusFailed    RunStatus = "failed"
	RunStatusCancelled RunStatus = "cancelled"
	RunStatusTimeout   RunStatus = "timeout"
)

// ScheduleType describes how a job's next execution time is computed.
type ScheduleType string

const (
	ScheduleTypeCron     ScheduleType = "cron"
	ScheduleTypeInterval ScheduleType = "interval"
	ScheduleTypeOneTime  ScheduleType = "one_time"
	ScheduleTypeManual   ScheduleType = "manual"
)

// TriggerSource records what initiated a particular run.
type TriggerSource string

const (
	TriggerScheduler TriggerSource = "scheduler"
	TriggerManual    TriggerSource = "manual"
	TriggerRetry     TriggerSource = "retry"
)

// RetryBackoff strategy for failed jobs.
type RetryBackoff string

const (
	BackoffFixed       RetryBackoff = "fixed"
	BackoffLinear      RetryBackoff = "linear"
	BackoffExponential RetryBackoff = "exponential"
)

// Job is the definition of a scheduled or async job stored in the database.
type Job struct {
	ID               string            `json:"id"`
	Name             string            `json:"name"`
	Description      string            `json:"description"`
	JobType          string            `json:"job_type"`
	ScheduleType     ScheduleType      `json:"schedule_type"`
	CronExpression   string            `json:"cron_expression,omitempty"`
	IntervalSeconds  int               `json:"interval_seconds,omitempty"`
	RunAt            *time.Time        `json:"run_at,omitempty"`
	Payload          map[string]any    `json:"payload"`
	Timezone         string            `json:"timezone"`
	MaxRetries       int               `json:"max_retries"`
	RetryDelaySeconds int              `json:"retry_delay_seconds"`
	TimeoutSeconds   int               `json:"timeout_seconds"`
	IsEnabled        bool              `json:"is_enabled"`
	IsSingleton      bool              `json:"is_singleton"`
	Tags             []string          `json:"tags"`
	LastRunAt        *time.Time        `json:"last_run_at,omitempty"`
	NextRunAt        *time.Time        `json:"next_run_at,omitempty"`
	LastRunStatus    string            `json:"last_run_status,omitempty"`
	RunCount         int64             `json:"run_count"`
	SuccessCount     int64             `json:"success_count"`
	FailureCount     int64             `json:"failure_count"`
	CreatedAt        string            `json:"created_at"`
	UpdatedAt        string            `json:"updated_at"`
}

// JobRun represents a single execution of a job.
type JobRun struct {
	ID           string        `json:"id"`
	JobID        string        `json:"job_id"`
	JobName      string        `json:"job_name,omitempty"`
	Status       RunStatus     `json:"status"`
	TriggeredBy  TriggerSource `json:"triggered_by"`
	Attempt      int           `json:"attempt"`
	StartedAt    *time.Time    `json:"started_at,omitempty"`
	FinishedAt   *time.Time    `json:"finished_at,omitempty"`
	DurationMs   int64         `json:"duration_ms"`
	Output       string        `json:"output,omitempty"`
	ErrorMessage string        `json:"error_message,omitempty"`
	Host         string        `json:"host,omitempty"`
	CreatedAt    string        `json:"created_at"`
}

// SchedulerSettings holds all professional scheduler configuration keys.
type SchedulerSettings struct {
	MaxConcurrentJobs int          `json:"max_concurrent_jobs"`
	JobTimeout        int          `json:"job_timeout"` // seconds
	MaxRetries        int          `json:"max_retries"`
	RetryBackoff      RetryBackoff `json:"retry_backoff"`
	RetryDelay        int          `json:"retry_delay"` // seconds
	DLQEnabled        bool         `json:"dlq_enabled"`
	DLQTTL            int          `json:"dlq_ttl"` // seconds
	Timezone          string       `json:"timezone"`
	SingletonMode     bool         `json:"singleton_mode"`
	Paused            bool         `json:"paused"`
	LogRetentionDays  int          `json:"log_retention_days"`
	WorkerPoolSize    int          `json:"worker_pool_size"`
	QueueKey          string       `json:"queue_key"`
	DLQKey            string       `json:"dlq_key"`
	HeartbeatInterval int          `json:"heartbeat_interval"` // seconds
}

// DefaultSettings returns a SchedulerSettings with sensible defaults.
func DefaultSettings() SchedulerSettings {
	return SchedulerSettings{
		MaxConcurrentJobs: 10,
		JobTimeout:        300,
		MaxRetries:        3,
		RetryBackoff:      BackoffExponential,
		RetryDelay:        30,
		DLQEnabled:        true,
		DLQTTL:            604800, // 7 days
		Timezone:          "UTC",
		SingletonMode:     true,
		Paused:            false,
		LogRetentionDays:  30,
		WorkerPoolSize:    5,
		QueueKey:          "cag:jobs:queue",
		DLQKey:            "cag:jobs:dlq",
		HeartbeatInterval: 30,
	}
}

// QueueMessage is the payload pushed to the Redis async queue.
type QueueMessage struct {
	JobID     string         `json:"job_id"`
	JobType   string         `json:"job_type"`
	Payload   map[string]any `json:"payload"`
	RunID     string         `json:"run_id"`
	Attempt   int            `json:"attempt"`
	EnqueuedAt time.Time     `json:"enqueued_at"`
}

// ExecutorFunc is the signature for all job executor functions.
// It receives the job context, payload, and returns optional output text or an error.
type ExecutorFunc func(ctx *ExecutionContext) (output string, err error)

// ExecutionContext carries all context a job executor needs at runtime.
// Context is a timeout-aware context derived from the scheduler's tCtx —
// executors MUST use this (not context.Background()) so that when the
// scheduler timeout fires the underlying HTTP and DB operations cancel cleanly.
type ExecutionContext struct {
	Context context.Context // timeout-aware; cancelled when the job times out
	JobID   string
	RunID   string
	JobType string
	Payload map[string]any
	Attempt int
}
