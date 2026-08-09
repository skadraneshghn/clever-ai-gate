package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// Queue is a Redis-backed async job queue.
// Jobs are pushed as JSON to a Redis list (LPUSH) and popped by workers (BRPOP).
// Failed jobs exceeding max retries are pushed to a Dead-Letter Queue.
type Queue struct {
	rdb      *redis.Client
	settings SchedulerSettings
	logger   *zap.Logger
}

// NewQueue creates a new Redis queue instance.
func NewQueue(rdb *redis.Client, settings SchedulerSettings, logger *zap.Logger) *Queue {
	return &Queue{
		rdb:      rdb,
		settings: settings,
		logger:   logger,
	}
}

// Enqueue pushes a job message onto the Redis queue.
func (q *Queue) Enqueue(ctx context.Context, msg QueueMessage) error {
	if msg.EnqueuedAt.IsZero() {
		msg.EnqueuedAt = time.Now()
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal queue message: %w", err)
	}

	if err := q.rdb.LPush(ctx, q.settings.QueueKey, data).Err(); err != nil {
		return fmt.Errorf("failed to push to queue: %w", err)
	}

	q.logger.Debug("job enqueued",
		zap.String("job_id", msg.JobID),
		zap.String("run_id", msg.RunID),
		zap.String("job_type", msg.JobType),
		zap.Int("attempt", msg.Attempt),
	)
	return nil
}

// Dequeue blocks until a job message is available, with the given timeout.
// Returns nil, nil when the context is cancelled or timeout occurs.
func (q *Queue) Dequeue(ctx context.Context, timeout time.Duration) (*QueueMessage, error) {
	result, err := q.rdb.BRPop(ctx, timeout, q.settings.QueueKey).Result()
	if err != nil {
		if err == redis.Nil || err == context.Canceled || err == context.DeadlineExceeded {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to dequeue: %w", err)
	}

	if len(result) < 2 {
		return nil, nil
	}

	var msg QueueMessage
	if err := json.Unmarshal([]byte(result[1]), &msg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal queue message: %w", err)
	}
	return &msg, nil
}

// SendToDLQ pushes a failed message to the Dead-Letter Queue with a TTL.
func (q *Queue) SendToDLQ(ctx context.Context, msg QueueMessage, failReason string) error {
	if !q.settings.DLQEnabled {
		q.logger.Debug("DLQ disabled — dropping failed job",
			zap.String("job_id", msg.JobID),
			zap.String("run_id", msg.RunID),
		)
		return nil
	}

	type DLQEntry struct {
		QueueMessage
		FailReason  string    `json:"fail_reason"`
		FailedAt    time.Time `json:"failed_at"`
	}

	entry := DLQEntry{
		QueueMessage: msg,
		FailReason:   failReason,
		FailedAt:     time.Now(),
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("failed to marshal DLQ entry: %w", err)
	}

	if err := q.rdb.LPush(ctx, q.settings.DLQKey, data).Err(); err != nil {
		return fmt.Errorf("failed to push to DLQ: %w", err)
	}

	// Trim DLQ to prevent unbounded growth (keep last 10000 entries)
	q.rdb.LTrim(ctx, q.settings.DLQKey, 0, 9999)

	q.logger.Warn("job sent to DLQ",
		zap.String("job_id", msg.JobID),
		zap.String("run_id", msg.RunID),
		zap.String("fail_reason", failReason),
	)
	return nil
}

// QueueLength returns the number of pending items in the async queue.
func (q *Queue) QueueLength(ctx context.Context) (int64, error) {
	return q.rdb.LLen(ctx, q.settings.QueueKey).Result()
}

// DLQLength returns the number of items in the dead-letter queue.
func (q *Queue) DLQLength(ctx context.Context) (int64, error) {
	return q.rdb.LLen(ctx, q.settings.DLQKey).Result()
}

// PurgeDLQ removes all entries from the dead-letter queue.
func (q *Queue) PurgeDLQ(ctx context.Context) error {
	return q.rdb.Del(ctx, q.settings.DLQKey).Err()
}

// PeekDLQ returns the last N entries from the DLQ without removing them.
func (q *Queue) PeekDLQ(ctx context.Context, limit int64) ([]string, error) {
	return q.rdb.LRange(ctx, q.settings.DLQKey, 0, limit-1).Result()
}
