package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	gocronredislock "github.com/go-co-op/gocron-redis-lock/v2"
	"github.com/go-co-op/gocron/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/skadraneshghn/clever-ai-gate/internal/credentials"
	"go.uber.org/zap"
)

// Scheduler is the central job scheduling engine.
// It wraps gocron v2 with Redis-based distributed locking, an async queue,
// a worker pool, and PostgreSQL-persisted job definitions.
type Scheduler struct {
	scheduler gocron.Scheduler
	registry  *Registry
	settings  *SettingsStore
	queue     *Queue
	worker    *Worker
	db        *pgxpool.Pool
	rdb       *redis.Client
	vault     *credentials.Vault
	logger    *zap.Logger
	cfg       SchedulerSettings

	mu       sync.RWMutex
	jobMap   map[string]gocron.Job // DB job ID → gocron job
	hostname string
}

// NewScheduler creates and initializes the Scheduler.
// rdb may be nil — in that case distributed locking and the async queue are disabled.
func NewScheduler(db *pgxpool.Pool, rdb *redis.Client, vault *credentials.Vault, logger *zap.Logger) (*Scheduler, error) {
	hostname, _ := os.Hostname()

	settingsStore := NewSettingsStore(db, logger)
	ctx := context.Background()
	cfg, err := settingsStore.Load(ctx)
	if err != nil {
		logger.Warn("failed to load scheduler settings, using defaults", zap.Error(err))
		cfg = DefaultSettings()
	}

	registry := NewRegistry()

	// Build gocron scheduler options
	opts := []gocron.SchedulerOption{
		gocron.WithLimitConcurrentJobs(uint(cfg.MaxConcurrentJobs), gocron.LimitModeWait),
	}

	// Parse timezone
	loc, tzErr := time.LoadLocation(cfg.Timezone)
	if tzErr == nil {
		opts = append(opts, gocron.WithLocation(loc))
	} else {
		logger.Warn("invalid scheduler timezone, using UTC", zap.String("tz", cfg.Timezone), zap.Error(tzErr))
	}

	// Enable distributed elector when Redis is available
	if rdb != nil {
		locker, lockErr := gocronredislock.NewRedisLocker(rdb, gocronredislock.WithTries(1))
		if lockErr == nil {
			opts = append(opts, gocron.WithDistributedLocker(locker))
			logger.Info("distributed job locking enabled via Redis")
		} else {
			logger.Warn("failed to create Redis locker, running without distributed locking", zap.Error(lockErr))
		}
	}

	s, err := gocron.NewScheduler(opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create gocron scheduler: %w", err)
	}

	// Build queue and worker (only with Redis)
	var q *Queue
	var w *Worker
	if rdb != nil {
		q = NewQueue(rdb, cfg, logger)
		w = NewWorker(q, registry, db, cfg, logger)
	}

	sched := &Scheduler{
		scheduler: s,
		registry:  registry,
		settings:  settingsStore,
		queue:     q,
		worker:    w,
		db:        db,
		rdb:       rdb,
		vault:     vault,
		logger:    logger,
		cfg:       cfg,
		jobMap:    make(map[string]gocron.Job),
		hostname:  hostname,
	}

	return sched, nil
}

// Registry returns the job type registry.
func (s *Scheduler) Registry() *Registry {
	return s.registry
}

// Settings returns the settings store.
func (s *Scheduler) Settings() *SettingsStore {
	return s.settings
}

// Config returns the current scheduler configuration.
func (s *Scheduler) Config() SchedulerSettings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg
}

// Start initializes all scheduled jobs from the database and begins scheduling.
func (s *Scheduler) Start(ctx context.Context) error {
	// Register built-in executors (vault forwarded for bulk health check)
	RegisterBuiltinExecutors(s.registry, s.db, s.vault, s.logger)

	// Load jobs from DB and register them
	if err := s.loadJobsFromDB(ctx); err != nil {
		return fmt.Errorf("failed to load jobs from DB: %w", err)
	}

	// Start the gocron scheduler
	s.scheduler.Start()

	// Start async worker pool if Redis is available
	if s.worker != nil {
		s.worker.Start()
	}

	s.logger.Info("job scheduler started",
		zap.Bool("redis_enabled", s.rdb != nil),
		zap.Bool("async_queue_enabled", s.queue != nil),
		zap.String("timezone", s.cfg.Timezone),
		zap.Int("max_concurrent_jobs", s.cfg.MaxConcurrentJobs),
	)
	return nil
}

// Stop shuts down the scheduler and all workers gracefully.
func (s *Scheduler) Stop() error {
	if s.worker != nil {
		s.worker.Stop()
	}
	err := s.scheduler.Shutdown()
	s.logger.Info("job scheduler stopped")
	return err
}

// loadJobsFromDB reads all enabled jobs from the database and registers them with gocron.
func (s *Scheduler) loadJobsFromDB(ctx context.Context) error {
	rows, err := s.db.Query(ctx, `
		SELECT id, name, job_type, schedule_type, cron_expression,
		       interval_seconds, run_at, payload, timezone,
		       timeout_seconds, is_enabled, is_singleton
		FROM jobs
		WHERE is_enabled = TRUE
		ORDER BY created_at
	`)
	if err != nil {
		return fmt.Errorf("failed to query jobs: %w", err)
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var j Job
		var payloadJSON []byte
		var cronExpr, tz *string
		var intervalSec *int
		var runAt *time.Time

		if err := rows.Scan(
			&j.ID, &j.Name, &j.JobType, &j.ScheduleType,
			&cronExpr, &intervalSec, &runAt, &payloadJSON, &tz,
			&j.TimeoutSeconds, &j.IsEnabled, &j.IsSingleton,
		); err != nil {
			s.logger.Error("failed to scan job row", zap.Error(err))
			continue
		}

		if cronExpr != nil {
			j.CronExpression = *cronExpr
		}
		if intervalSec != nil {
			j.IntervalSeconds = *intervalSec
		}
		if runAt != nil {
			j.RunAt = runAt
		}
		if tz != nil {
			j.Timezone = *tz
		}

		if payloadJSON != nil {
			_ = json.Unmarshal(payloadJSON, &j.Payload)
		}
		if j.Payload == nil {
			j.Payload = make(map[string]any)
		}

		if err := s.scheduleJob(j); err != nil {
			s.logger.Error("failed to schedule job",
				zap.String("job_id", j.ID),
				zap.String("name", j.Name),
				zap.Error(err),
			)
			continue
		}
		count++
	}

	s.logger.Info("jobs loaded from database", zap.Int("count", count))
	return nil
}

// scheduleJob registers a single job with the gocron scheduler.
func (s *Scheduler) scheduleJob(j Job) error {
	if !s.registry.Has(j.JobType) {
		return fmt.Errorf("unknown job type %q — executor not registered", j.JobType)
	}

	var jobDef gocron.JobDefinition

	switch j.ScheduleType {
	case ScheduleTypeCron:
		if j.CronExpression == "" {
			return fmt.Errorf("cron expression is required for cron schedule type")
		}
		jobDef = gocron.CronJob(j.CronExpression, false)

	case ScheduleTypeInterval:
		if j.IntervalSeconds <= 0 {
			return fmt.Errorf("interval_seconds must be > 0 for interval schedule type")
		}
		jobDef = gocron.DurationJob(time.Duration(j.IntervalSeconds) * time.Second)

	case ScheduleTypeOneTime:
		if j.RunAt == nil {
			return fmt.Errorf("run_at is required for one_time schedule type")
		}
		jobDef = gocron.OneTimeJob(gocron.OneTimeJobStartDateTime(*j.RunAt))

	case ScheduleTypeManual:
		// Manual jobs are not registered with gocron — triggered on demand only
		return nil

	default:
		return fmt.Errorf("unknown schedule type: %s", j.ScheduleType)
	}

	// Build the task function
	task := gocron.NewTask(s.makeTaskFunc(j))

	// Job options
	jobOpts := []gocron.JobOption{
		gocron.WithName(j.Name),
		gocron.WithTags(append(j.Tags, j.ID)...),
	}
	if j.IsSingleton {
		jobOpts = append(jobOpts, gocron.WithSingletonMode(gocron.LimitModeWait))
	}

	gJob, err := s.scheduler.NewJob(jobDef, task, jobOpts...)
	if err != nil {
		return fmt.Errorf("failed to create gocron job: %w", err)
	}

	s.mu.Lock()
	s.jobMap[j.ID] = gJob
	s.mu.Unlock()

	s.logger.Info("job scheduled",
		zap.String("job_id", j.ID),
		zap.String("name", j.Name),
		zap.String("type", j.JobType),
		zap.String("schedule", string(j.ScheduleType)),
	)
	return nil
}

// makeTaskFunc returns a closure that creates a DB run record and executes the job.
func (s *Scheduler) makeTaskFunc(j Job) func() {
	return func() {
		if s.cfg.Paused {
			s.logger.Debug("scheduler is paused, skipping job", zap.String("job_id", j.ID))
			return
		}

		ctx := context.Background()
		runID := uuid.New().String()

		// Create run record
		_, err := s.db.Exec(ctx, `
			INSERT INTO job_runs (id, job_id, status, triggered_by, attempt, host, created_at)
			VALUES ($1, $2, 'running', 'scheduler', 1, $3, NOW())
		`, runID, j.ID, s.hostname)
		if err != nil {
			s.logger.Error("failed to create job run record", zap.String("job_id", j.ID), zap.Error(err))
		}

		// Update job last_run_at
		_, _ = s.db.Exec(ctx, `
			UPDATE jobs SET last_run_at = NOW(), last_run_status = 'running',
			run_count = run_count + 1, updated_at = NOW()
			WHERE id = $1
		`, j.ID)

		// Execute
		executor, _ := s.registry.Get(j.JobType)
		execCtx := &ExecutionContext{
			JobID:   j.ID,
			RunID:   runID,
			JobType: j.JobType,
			Payload: j.Payload,
			Attempt: 1,
		}

		timeout := time.Duration(j.TimeoutSeconds) * time.Second
		if timeout <= 0 {
			timeout = 5 * time.Minute
		}
		tCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		start := time.Now()
		var output string
		var execErr error

		done := make(chan struct{})
		go func() {
			defer close(done)
			defer func() {
				if r := recover(); r != nil {
					execErr = fmt.Errorf("job panicked: %v", r)
				}
			}()
			output, execErr = executor(execCtx)
		}()

		select {
		case <-tCtx.Done():
			execErr = fmt.Errorf("job timed out after %s", timeout)
		case <-done:
		}

		durationMs := time.Since(start).Milliseconds()

		if execErr != nil {
			s.logger.Error("scheduled job failed",
				zap.String("job_id", j.ID),
				zap.String("run_id", runID),
				zap.Int64("duration_ms", durationMs),
				zap.Error(execErr),
			)
			_, _ = s.db.Exec(ctx, `
				UPDATE job_runs SET status = 'failed', finished_at = NOW(),
				error_message = $2, duration_ms = $3 WHERE id = $1
			`, runID, execErr.Error(), durationMs)
			_, _ = s.db.Exec(ctx, `
				UPDATE jobs SET last_run_status = 'failed', failure_count = failure_count + 1,
				updated_at = NOW() WHERE id = $1
			`, j.ID)
		} else {
			s.logger.Info("scheduled job succeeded",
				zap.String("job_id", j.ID),
				zap.String("run_id", runID),
				zap.Int64("duration_ms", durationMs),
			)
			_, _ = s.db.Exec(ctx, `
				UPDATE job_runs SET status = 'success', finished_at = NOW(),
				output = $2, duration_ms = $3 WHERE id = $1
			`, runID, output, durationMs)
			_, _ = s.db.Exec(ctx, `
				UPDATE jobs SET last_run_status = 'success', success_count = success_count + 1,
				updated_at = NOW() WHERE id = $1
			`, j.ID)
		}
	}
}

// TriggerNow immediately executes a job by ID, creating an async run record.
func (s *Scheduler) TriggerNow(ctx context.Context, jobID string) (string, error) {
	// Fetch job from DB
	var j Job
	var payloadJSON []byte
	var cronExpr, tz *string

	err := s.db.QueryRow(ctx, `
		SELECT id, name, job_type, schedule_type, cron_expression, payload, timezone,
		       timeout_seconds, is_enabled
		FROM jobs WHERE id = $1
	`, jobID).Scan(
		&j.ID, &j.Name, &j.JobType, &j.ScheduleType,
		&cronExpr, &payloadJSON, &tz, &j.TimeoutSeconds, &j.IsEnabled,
	)
	if err != nil {
		return "", fmt.Errorf("job not found: %w", err)
	}

	if payloadJSON != nil {
		_ = json.Unmarshal(payloadJSON, &j.Payload)
	}
	if j.Payload == nil {
		j.Payload = make(map[string]any)
	}

	runID := uuid.New().String()

	// Create pending run record
	_, err = s.db.Exec(ctx, `
		INSERT INTO job_runs (id, job_id, status, triggered_by, attempt, host, started_at, created_at)
		VALUES ($1, $2, 'pending', 'manual', 1, $3, NOW(), NOW())
	`, runID, jobID, s.hostname)
	if err != nil {
		return "", fmt.Errorf("failed to create run record: %w", err)
	}

	// Queue for async execution or run inline
	if s.queue != nil {
		msg := QueueMessage{
			JobID:      jobID,
			JobType:    j.JobType,
			Payload:    j.Payload,
			RunID:      runID,
			Attempt:    1,
			EnqueuedAt: time.Now(),
		}
		if err := s.queue.Enqueue(ctx, msg); err != nil {
			return runID, fmt.Errorf("failed to enqueue job: %w", err)
		}
	} else {
		// No Redis — run synchronously in a goroutine
		jCopy := j
		rID := runID
		go s.makeTaskFuncWithRunID(jCopy, rID)()
	}

	return runID, nil
}

// makeTaskFuncWithRunID runs a job with a pre-created run record.
func (s *Scheduler) makeTaskFuncWithRunID(j Job, runID string) func() {
	return func() {
		ctx := context.Background()
		executor, ok := s.registry.Get(j.JobType)
		if !ok {
			s.logger.Error("no executor for manual trigger", zap.String("job_type", j.JobType))
			_, _ = s.db.Exec(ctx, `UPDATE job_runs SET status = 'failed', error_message = $2, finished_at = NOW() WHERE id = $1`,
				runID, "unknown job type")
			return
		}

		execCtx := &ExecutionContext{JobID: j.ID, RunID: runID, JobType: j.JobType, Payload: j.Payload, Attempt: 1}
		timeout := time.Duration(j.TimeoutSeconds) * time.Second
		if timeout <= 0 {
			timeout = 5 * time.Minute
		}
		tCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		start := time.Now()
		output, execErr := s.runWithTimeout(tCtx, executor, execCtx)
		durationMs := time.Since(start).Milliseconds()

		if execErr != nil {
			_, _ = s.db.Exec(ctx, `UPDATE job_runs SET status = 'failed', finished_at = NOW(), error_message = $2, duration_ms = $3 WHERE id = $1`,
				runID, execErr.Error(), durationMs)
		} else {
			_, _ = s.db.Exec(ctx, `UPDATE job_runs SET status = 'success', finished_at = NOW(), output = $2, duration_ms = $3 WHERE id = $1`,
				runID, output, durationMs)
		}
	}
}

func (s *Scheduler) runWithTimeout(ctx context.Context, fn ExecutorFunc, execCtx *ExecutionContext) (string, error) {
	type result struct {
		output string
		err    error
	}
	ch := make(chan result, 1)
	go func() {
		o, e := fn(execCtx)
		ch <- result{o, e}
	}()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case r := <-ch:
		return r.output, r.err
	}
}

// RegisterJobInGocron adds a newly created DB job to the live gocron scheduler.
func (s *Scheduler) RegisterJobInGocron(j Job) error {
	return s.scheduleJob(j)
}

// UnregisterJobFromGocron removes a job from the live gocron scheduler by DB job ID.
func (s *Scheduler) UnregisterJobFromGocron(jobID string) error {
	s.mu.Lock()
	gJob, ok := s.jobMap[jobID]
	if ok {
		delete(s.jobMap, jobID)
	}
	s.mu.Unlock()

	if !ok {
		return nil // Already not scheduled
	}

	return s.scheduler.RemoveJob(gJob.ID())
}

// Stats returns current scheduler runtime statistics.
func (s *Scheduler) Stats(ctx context.Context) (map[string]any, error) {
	stats := map[string]any{}

	var totalJobs, enabledJobs int64
	_ = s.db.QueryRow(ctx, `SELECT COUNT(*), COUNT(*) FILTER (WHERE is_enabled = TRUE) FROM jobs`).Scan(&totalJobs, &enabledJobs)

	var runningJobs, pendingJobs, completedJobs, failedJobs int64
	_ = s.db.QueryRow(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE status = 'running'),
			COUNT(*) FILTER (WHERE status = 'pending'),
			COUNT(*) FILTER (WHERE status = 'success'),
			COUNT(*) FILTER (WHERE status = 'failed')
		FROM job_runs
		WHERE created_at > NOW() - INTERVAL '24 hours'
	`).Scan(&runningJobs, &pendingJobs, &completedJobs, &failedJobs)

	stats["total_jobs"] = totalJobs
	stats["enabled_jobs"] = enabledJobs
	stats["running_24h"] = runningJobs
	stats["pending_24h"] = pendingJobs
	stats["completed_24h"] = completedJobs
	stats["failed_24h"] = failedJobs
	stats["scheduler_paused"] = s.cfg.Paused
	stats["redis_enabled"] = s.rdb != nil
	stats["registered_job_types"] = s.registry.ListTypes()
	stats["max_concurrent_jobs"] = s.cfg.MaxConcurrentJobs
	stats["worker_pool_size"] = s.cfg.WorkerPoolSize

	if s.queue != nil {
		qLen, _ := s.queue.QueueLength(ctx)
		dlqLen, _ := s.queue.DLQLength(ctx)
		stats["queue_depth"] = qLen
		stats["dlq_depth"] = dlqLen
	}

	return stats, nil
}

// ReloadSettings reloads scheduler settings from the database and applies changes.
func (s *Scheduler) ReloadSettings(ctx context.Context) error {
	newCfg, err := s.settings.Load(ctx)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.cfg = newCfg
	s.mu.Unlock()
	s.logger.Info("scheduler settings reloaded")
	return nil
}
