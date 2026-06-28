package telemetry

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

// LogEntry represents a single request telemetry record.
// Allocated on the hot-path but immediately sent to the async channel
// so the proxy handler is never blocked by database writes.
type LogEntry struct {
	TenantID         string
	Model            string
	Provider         string
	PromptTokens     int
	CompletionTokens int
	LatencyMs        int
	StatusCode       int
	ErrorMessage     string
	CreatedAt        time.Time
}

// Pipeline is the async write-behind telemetry system.
// It decouples database writes from the request hot-path using a buffered
// channel and background worker goroutine.
//
// Design:
//   - Buffered channel (default 10,000): provides backpressure relief
//   - Non-blocking send: drops entries under extreme load rather than blocking proxy
//   - Bulk insert: batches entries into a single multi-row INSERT for efficiency
//   - Time-based flush: ensures data reaches the DB even during low traffic
type Pipeline struct {
	queue     chan *LogEntry
	pool      *pgxpool.Pool
	logger    *zap.Logger
	batchSize int
	flushInterval time.Duration
	ctx       context.Context
	cancel    context.CancelFunc
}

// NewPipeline creates a new async telemetry pipeline.
func NewPipeline(pool *pgxpool.Pool, logger *zap.Logger, queueSize, batchSize int, flushInterval time.Duration) *Pipeline {
	ctx, cancel := context.WithCancel(context.Background())
	return &Pipeline{
		queue:         make(chan *LogEntry, queueSize),
		pool:          pool,
		logger:        logger,
		batchSize:     batchSize,
		flushInterval: flushInterval,
		ctx:           ctx,
		cancel:        cancel,
	}
}

// Start launches the background worker goroutine.
func (p *Pipeline) Start() {
	go p.worker()
	p.logger.Info("telemetry pipeline started",
		zap.Int("queue_size", cap(p.queue)),
		zap.Int("batch_size", p.batchSize),
		zap.Duration("flush_interval", p.flushInterval),
	)
}

// Emit sends a log entry to the pipeline.
// This is called from the hot-path and uses non-blocking send
// to guarantee the proxy handler is never blocked by telemetry.
func (p *Pipeline) Emit(entry *LogEntry) {
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now()
	}
	select {
	case p.queue <- entry:
		// Sent successfully
	default:
		// Queue full — drop the entry rather than blocking the proxy
		p.logger.Debug("telemetry queue full, dropping entry")
	}
}

// Stop gracefully shuts down the pipeline, flushing remaining entries.
func (p *Pipeline) Stop() {
	p.cancel()
	// Drain remaining entries
	close(p.queue)
}

// worker is the background goroutine that batches and flushes entries.
func (p *Pipeline) worker() {
	batch := make([]*LogEntry, 0, p.batchSize)
	ticker := time.NewTicker(p.flushInterval)
	defer ticker.Stop()

	for {
		select {
		case entry, ok := <-p.queue:
			if !ok {
				// Channel closed — flush remaining and exit
				if len(batch) > 0 {
					p.flush(batch)
				}
				return
			}
			batch = append(batch, entry)
			if len(batch) >= p.batchSize {
				p.flush(batch)
				batch = batch[:0]
			}

		case <-ticker.C:
			if len(batch) > 0 {
				p.flush(batch)
				batch = batch[:0]
			}

		case <-p.ctx.Done():
			// Context cancelled — flush remaining
			if len(batch) > 0 {
				p.flush(batch)
			}
			return
		}
	}
}

// flush performs a bulk INSERT of the accumulated batch.
// Uses a multi-row VALUES clause for efficiency.
func (p *Pipeline) flush(batch []*LogEntry) {
	if len(batch) == 0 {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Build multi-row INSERT
	var sb strings.Builder
	sb.WriteString(`INSERT INTO request_logs (tenant_id, model, provider, prompt_tokens, completion_tokens, latency_ms, status_code, error_message, created_at) VALUES `)

	args := make([]interface{}, 0, len(batch)*9)
	for i, entry := range batch {
		if i > 0 {
			sb.WriteString(",")
		}
		base := i * 9
		sb.WriteString(fmt.Sprintf("($%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d)",
			base+1, base+2, base+3, base+4, base+5, base+6, base+7, base+8, base+9))

		var tenantID interface{}
		if entry.TenantID != "" {
			tenantID = entry.TenantID
		}

		var errMsg interface{}
		if entry.ErrorMessage != "" {
			errMsg = entry.ErrorMessage
		}

		args = append(args,
			tenantID,
			entry.Model,
			entry.Provider,
			entry.PromptTokens,
			entry.CompletionTokens,
			entry.LatencyMs,
			entry.StatusCode,
			errMsg,
			entry.CreatedAt,
		)
	}

	_, err := p.pool.Exec(ctx, sb.String(), args...)
	if err != nil {
		p.logger.Error("failed to flush telemetry batch",
			zap.Int("batch_size", len(batch)),
			zap.Error(err),
		)
		return
	}

	p.logger.Debug("telemetry batch flushed", zap.Int("count", len(batch)))
}
