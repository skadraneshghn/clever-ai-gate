package jobs

import (
	"context"
	"fmt"
	"math"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

// Worker drains the Redis async queue and executes jobs using registered executors.
type Worker struct {
	queue    *Queue
	registry *Registry
	db       *pgxpool.Pool
	settings SchedulerSettings
	logger   *zap.Logger
	host     string
	stopCh   chan struct{}
}

// NewWorker creates a new worker pool manager.
func NewWorker(queue *Queue, registry *Registry, db *pgxpool.Pool, settings SchedulerSettings, logger *zap.Logger) *Worker {
	hostname, _ := os.Hostname()
	return &Worker{
		queue:    queue,
		registry: registry,
		db:       db,
		settings: settings,
		logger:   logger,
		host:     hostname,
		stopCh:   make(chan struct{}),
	}
}

// Start launches the configured number of worker goroutines.
func (w *Worker) Start() {
	count := w.settings.WorkerPoolSize
	if count < 1 {
		count = 5
	}

	w.logger.Info("starting async job workers", zap.Int("workers", count))
	for i := 0; i < count; i++ {
		go w.run(i)
	}
}

// Stop signals all workers to stop after finishing their current job.
func (w *Worker) Stop() {
	close(w.stopCh)
}

// run is the inner loop for a single worker goroutine.
func (w *Worker) run(workerID int) {
	w.logger.Debug("worker started", zap.Int("worker_id", workerID))
	ctx := context.Background()

	for {
		select {
		case <-w.stopCh:
			w.logger.Debug("worker stopped", zap.Int("worker_id", workerID))
			return
		default:
		}

		// Block-wait up to 2 seconds for a job
		msg, err := w.queue.Dequeue(ctx, 2*time.Second)
		if err != nil {
			w.logger.Error("worker dequeue error", zap.Int("worker_id", workerID), zap.Error(err))
			time.Sleep(500 * time.Millisecond)
			continue
		}
		if msg == nil {
			// Timeout — check stop signal and loop
			continue
		}

		w.processMessage(ctx, workerID, *msg)
	}
}

// processMessage executes a single job message.
func (w *Worker) processMessage(ctx context.Context, workerID int, msg QueueMessage) {
	log := w.logger.With(
		zap.String("job_id", msg.JobID),
		zap.String("run_id", msg.RunID),
		zap.String("job_type", msg.JobType),
		zap.Int("attempt", msg.Attempt),
		zap.Int("worker_id", workerID),
	)

	log.Info("worker processing job")

	// Fetch executor
	executor, ok := w.registry.Get(msg.JobType)
	if !ok {
		errMsg := fmt.Sprintf("unknown job type: %s", msg.JobType)
		log.Error("no executor for job type", zap.String("job_type", msg.JobType))
		w.markRunFailed(ctx, msg.RunID, errMsg)
		w.sendToDLQ(ctx, msg, errMsg)
		return
	}

	// Mark run as started
	w.markRunStarted(ctx, msg.RunID)

	// Build execution context with timeout
	timeout := time.Duration(w.settings.JobTimeout) * time.Second
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()
	execContext := &ExecutionContext{
		JobID:   msg.JobID,
		RunID:   msg.RunID,
		JobType: msg.JobType,
		Payload: msg.Payload,
		Attempt: msg.Attempt,
	}

	// Execute the job
	output, err := w.runWithContext(execCtx, executor, execContext)
	durationMs := time.Since(start).Milliseconds()

	if err != nil {
		log.Error("job execution failed",
			zap.Error(err),
			zap.Int64("duration_ms", durationMs),
		)

		// Retry logic
		if msg.Attempt < w.settings.MaxRetries {
			delay := w.computeRetryDelay(msg.Attempt)
			log.Info("scheduling retry",
				zap.Int("next_attempt", msg.Attempt+1),
				zap.Duration("delay", delay),
			)
			go func() {
				time.Sleep(delay)
				retryMsg := msg
				retryMsg.Attempt++
				if qErr := w.queue.Enqueue(ctx, retryMsg); qErr != nil {
					log.Error("failed to enqueue retry", zap.Error(qErr))
				}
			}()
			w.markRunFailed(ctx, msg.RunID, err.Error())
		} else {
			// Exceeded max retries — send to DLQ
			w.markRunFailed(ctx, msg.RunID, err.Error())
			w.sendToDLQ(ctx, msg, err.Error())
		}

		w.updateJobStats(ctx, msg.JobID, false, durationMs)
		return
	}

	log.Info("job executed successfully", zap.Int64("duration_ms", durationMs))
	w.markRunSuccess(ctx, msg.RunID, output, durationMs)
	w.updateJobStats(ctx, msg.JobID, true, durationMs)
}

// runWithContext wraps executor call with context cancellation.
func (w *Worker) runWithContext(ctx context.Context, fn ExecutorFunc, execCtx *ExecutionContext) (output string, err error) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("job panicked: %v", r)
			}
		}()
		output, err = fn(execCtx)
	}()

	select {
	case <-ctx.Done():
		return "", fmt.Errorf("job timed out or cancelled: %w", ctx.Err())
	case <-done:
		return output, err
	}
}

// computeRetryDelay calculates the delay before a retry attempt.
func (w *Worker) computeRetryDelay(attempt int) time.Duration {
	base := time.Duration(w.settings.RetryDelay) * time.Second
	switch w.settings.RetryBackoff {
	case BackoffExponential:
		factor := math.Pow(2, float64(attempt))
		return time.Duration(float64(base) * factor)
	case BackoffLinear:
		return base * time.Duration(attempt+1)
	default: // fixed
		return base
	}
}

// --- DB helpers ---

func (w *Worker) markRunStarted(ctx context.Context, runID string) {
	_, err := w.db.Exec(ctx, `
		UPDATE job_runs SET status = 'running', started_at = NOW(), host = $2 WHERE id = $1
	`, runID, w.host)
	if err != nil {
		w.logger.Error("failed to mark run as started", zap.String("run_id", runID), zap.Error(err))
	}
}

func (w *Worker) markRunSuccess(ctx context.Context, runID, output string, durationMs int64) {
	_, err := w.db.Exec(ctx, `
		UPDATE job_runs
		SET status = 'success', finished_at = NOW(), output = $2, duration_ms = $3
		WHERE id = $1
	`, runID, output, durationMs)
	if err != nil {
		w.logger.Error("failed to mark run as success", zap.String("run_id", runID), zap.Error(err))
	}
}

func (w *Worker) markRunFailed(ctx context.Context, runID, errMsg string) {
	_, err := w.db.Exec(ctx, `
		UPDATE job_runs
		SET status = 'failed', finished_at = NOW(), error_message = $2
		WHERE id = $1
	`, runID, errMsg)
	if err != nil {
		w.logger.Error("failed to mark run as failed", zap.String("run_id", runID), zap.Error(err))
	}
}

func (w *Worker) updateJobStats(ctx context.Context, jobID string, success bool, _ int64) {
	if success {
		_, _ = w.db.Exec(ctx, `
			UPDATE jobs SET run_count = run_count + 1, success_count = success_count + 1,
			last_run_at = NOW(), last_run_status = 'success', updated_at = NOW()
			WHERE id = $1
		`, jobID)
	} else {
		_, _ = w.db.Exec(ctx, `
			UPDATE jobs SET run_count = run_count + 1, failure_count = failure_count + 1,
			last_run_at = NOW(), last_run_status = 'failed', updated_at = NOW()
			WHERE id = $1
		`, jobID)
	}
}

func (w *Worker) sendToDLQ(ctx context.Context, msg QueueMessage, reason string) {
	if err := w.queue.SendToDLQ(ctx, msg, reason); err != nil {
		w.logger.Error("failed to send to DLQ", zap.String("run_id", msg.RunID), zap.Error(err))
	}
}
