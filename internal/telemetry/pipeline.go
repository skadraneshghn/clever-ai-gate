package telemetry

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/skadraneshghn/clever-ai-gate/internal/database"
	"go.uber.org/zap"
)

// LogEntry represents a single request telemetry record.
// Allocated on the hot-path but immediately sent to the async channel
// so the proxy handler is never blocked by database writes.
type LogEntry struct {
	TenantID         string    `json:"tenant_id,omitempty"`
	Model            string    `json:"model"`
	Provider         string    `json:"provider"`
	PromptTokens     int       `json:"prompt_tokens"`
	CompletionTokens int       `json:"completion_tokens"`
	LatencyMs        int       `json:"latency_ms"`
	StatusCode       int       `json:"status_code"`
	ErrorMessage     string    `json:"error_message,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	Prompt           string    `json:"prompt,omitempty"`
	Response         string    `json:"response,omitempty"`
}

// Pipeline is the async write-behind telemetry system.
// It decouples database writes from the request hot-path using a buffered
// channel (local fallback) or a high-performance Redis queue.
type Pipeline struct {
	queue         chan *LogEntry
	pool          *pgxpool.Pool
	redisClient   *redis.Client
	logger        *zap.Logger
	batchSize     int
	flushInterval time.Duration
	ctx           context.Context
	cancel        context.CancelFunc
}

// NewPipeline creates a new async telemetry pipeline.
func NewPipeline(pool *pgxpool.Pool, logger *zap.Logger, queueSize, batchSize int, flushInterval time.Duration, redisURL string) *Pipeline {
	ctx, cancel := context.WithCancel(context.Background())
	var rdb *redis.Client
	if redisURL != "" {
		opt, err := redis.ParseURL(redisURL)
		if err == nil {
			rdb = redis.NewClient(opt)
			// Test ping connection
			pingCtx, pingCancel := context.WithTimeout(context.Background(), 1*time.Second)
			defer pingCancel()
			if err := rdb.Ping(pingCtx).Err(); err != nil {
				logger.Warn("failed to ping redis, falling back to local memory queue", zap.Error(err))
				rdb = nil
			} else {
				logger.Info("connected to Redis queue successfully")
			}
		} else {
			logger.Warn("failed to parse redis URL, falling back to local memory queue", zap.Error(err))
		}
	}

	return &Pipeline{
		queue:         make(chan *LogEntry, queueSize),
		pool:          pool,
		redisClient:   rdb,
		logger:        logger,
		batchSize:     batchSize,
		flushInterval: flushInterval,
		ctx:           ctx,
		cancel:        cancel,
	}
}

// Start launches the background worker goroutines.
func (p *Pipeline) Start() {
	// Worker for local Go channel fallback queue
	go func() {
		defer func() {
			if r := recover(); r != nil {
				p.logger.Error("recovered from telemetry worker panic", zap.Any("panic", r))
				go p.worker()
			}
		}()
		p.worker()
	}()

	// Worker for Redis queue if active
	if p.redisClient != nil {
		go func() {
			defer func() {
				if r := recover(); r != nil {
					p.logger.Error("recovered from redis telemetry worker panic", zap.Any("panic", r))
					// Restart worker
					p.Start()
				}
			}()
			p.redisWorker()
		}()
	}

	p.logger.Info("telemetry pipeline started",
		zap.Int("queue_size", cap(p.queue)),
		zap.Int("batch_size", p.batchSize),
		zap.Duration("flush_interval", p.flushInterval),
		zap.Bool("using_redis", p.redisClient != nil),
	)
}

// Emit sends a log entry to the pipeline.
func (p *Pipeline) Emit(entry *LogEntry) {
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now()
	}

	if p.redisClient != nil {
		data, err := json.Marshal(entry)
		if err == nil {
			// Non-blocking push to Redis with short timeout
			ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
			defer cancel()
			err = p.redisClient.LPush(ctx, "clever_ai_gate:telemetry_queue", data).Err()
			if err == nil {
				return
			}
			p.logger.Warn("redis push failed, falling back to memory queue", zap.Error(err))
		}
	}

	select {
	case p.queue <- entry:
		// Sent successfully
	default:
		// Queue full — drop the entry rather than blocking proxy
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

// flush performs a bulk INSERT of the accumulated batch into request_logs,
// and maps prompt/response text into request_vector_logs with calculated embeddings.
func (p *Pipeline) flush(batch []*LogEntry) {
	if len(batch) == 0 {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Execute inside a single transaction to ensure consistency and minimize DB roundtrips.
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		p.logger.Error("failed to begin telemetry transaction", zap.Error(err))
		return
	}
	defer tx.Rollback(ctx)

	for _, entry := range batch {
		var logID int64
		var tenantID interface{}
		if entry.TenantID != "" {
			tenantID = entry.TenantID
		}

		var errMsg interface{}
		if entry.ErrorMessage != "" {
			errMsg = entry.ErrorMessage
		}

		// Insert metadata log
		err := tx.QueryRow(ctx, `
			INSERT INTO request_logs (tenant_id, model, provider, prompt_tokens, completion_tokens, latency_ms, status_code, error_message, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
			RETURNING id
		`, tenantID, entry.Model, entry.Provider, entry.PromptTokens, entry.CompletionTokens, entry.LatencyMs, entry.StatusCode, errMsg, entry.CreatedAt).Scan(&logID)
		if err != nil {
			p.logger.Error("failed to insert request log telemetry row", zap.Error(err))
			continue
		}

		// Calculate deterministic vector embedding
		embedding := database.GenerateEmbedding(entry.Prompt)

		// Insert vector details
		_, err = tx.Exec(ctx, `
			INSERT INTO request_vector_logs (log_id, prompt_text, response_text, prompt_embedding)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (log_id) DO NOTHING
		`, logID, entry.Prompt, entry.Response, embedding)
		if err != nil {
			p.logger.Error("failed to insert request vector details", zap.Error(err))
		}
	}

	if err := tx.Commit(ctx); err != nil {
		p.logger.Error("failed to commit telemetry transaction", zap.Error(err))
		return
	}

	p.logger.Debug("telemetry batch flushed successfully", zap.Int("count", len(batch)))
}

// redisWorker pulls entries from Redis and flushes them in batches.
func (p *Pipeline) redisWorker() {
	batch := make([]*LogEntry, 0, p.batchSize)
	ticker := time.NewTicker(p.flushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-p.ctx.Done():
			if len(batch) > 0 {
				p.flush(batch)
			}
			return
		default:
		}

		// BRPop blocks until a log entry is pushed or the 1 second timeout expires
		res, err := p.redisClient.BRPop(p.ctx, 1*time.Second, "clever_ai_gate:telemetry_queue").Result()
		if err == nil && len(res) == 2 {
			var entry LogEntry
			if err := json.Unmarshal([]byte(res[1]), &entry); err == nil {
				batch = append(batch, &entry)
				if len(batch) >= p.batchSize {
					p.flush(batch)
					batch = batch[:0]
				}
			}
		}

		select {
		case <-ticker.C:
			if len(batch) > 0 {
				p.flush(batch)
				batch = batch[:0]
			}
		default:
		}
	}
}


