// Package cluster provides Redis Pub/Sub-based coordination between horizontal
// gateway nodes. When one node penalizes an upstream API key (due to 429/5xx),
// it broadcasts the event so all other nodes instantly stop using that key.
//
// The golden rule is preserved: this never blocks the proxy hot-path.
// All publish operations are fire-and-forget goroutines.
package cluster

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

const clusterChannel = "clever_gate:cluster_events"

// EventType describes what happened to a credential across the cluster.
type EventType string

const (
	EventPenalize EventType = "penalize" // Key is rate-limited or erroring
	EventReset    EventType = "reset"    // Key is healthy again
)

// ClusterEvent is the payload broadcast over Redis Pub/Sub.
type ClusterEvent struct {
	Event       EventType `json:"event"`
	PoolPattern string    `json:"pool_pattern"`
	CredID      int       `json:"cred_id"`
	CredIndex   int       `json:"cred_index"`
	UntilNs     int64     `json:"until_ns,omitempty"` // Unix nanoseconds for penalize
}

// PoolLookup is a function that resolves a pool by model pattern.
// Implemented by credentials.SyncManager.GetPool.
type PoolLookup interface {
	GetPoolByPattern(pattern string) PoolPenalizer
}

// PoolPenalizer is the interface the broadcaster uses to update local pool state.
type PoolPenalizer interface {
	PenalizeByCredID(credID int, until int64)
	ResetByCredID(credID int)
}

// Broadcaster publishes and subscribes to cluster events.
// When nil (Redis not configured), all operations are no-ops.
type Broadcaster struct {
	rdb    *redis.Client
	logger *zap.Logger
	ctx    context.Context
	cancel context.CancelFunc
	mu     sync.Mutex
}

// New creates a Broadcaster. Returns nil (safe no-op) if rdb is nil.
func New(rdb *redis.Client, logger *zap.Logger) *Broadcaster {
	if rdb == nil {
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Broadcaster{
		rdb:    rdb,
		logger: logger,
		ctx:    ctx,
		cancel: cancel,
	}
}

// PublishPenalize asynchronously broadcasts a penalize event.
// Called by proxy handler after PenalizeToken — never blocks.
func (b *Broadcaster) PublishPenalize(poolPattern string, credID, credIndex int, until time.Time) {
	if b == nil {
		return
	}
	event := ClusterEvent{
		Event:       EventPenalize,
		PoolPattern: poolPattern,
		CredID:      credID,
		CredIndex:   credIndex,
		UntilNs:     until.UnixNano(),
	}
	go b.publish(event)
}

// PublishReset asynchronously broadcasts a reset event.
// Called after a successful request resets a credential's cooldown.
func (b *Broadcaster) PublishReset(poolPattern string, credID, credIndex int) {
	if b == nil {
		return
	}
	event := ClusterEvent{
		Event:       EventReset,
		PoolPattern: poolPattern,
		CredID:      credID,
		CredIndex:   credIndex,
	}
	go b.publish(event)
}

func (b *Broadcaster) publish(event ClusterEvent) {
	data, err := json.Marshal(event)
	if err != nil {
		b.logger.Error("failed to marshal cluster event", zap.Error(err))
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	if err := b.rdb.Publish(ctx, clusterChannel, data).Err(); err != nil {
		b.logger.Debug("cluster event publish failed (non-critical)", zap.Error(err))
	}
}

// StartSubscriber starts a background goroutine that receives cluster events
// and applies them to the local credential pool state.
// getPool is called to look up the pool by pattern for local mutation.
func (b *Broadcaster) StartSubscriber(getPool func(pattern string) PenalizerPool) {
	if b == nil {
		return
	}
	go func() {
		defer func() {
			if r := recover(); r != nil {
				b.logger.Error("recovered from cluster subscriber panic", zap.Any("panic", r))
				time.Sleep(2 * time.Second)
				b.StartSubscriber(getPool)
			}
		}()
		b.subscribeLoop(getPool)
	}()
	b.logger.Info("cluster event subscriber started", zap.String("channel", clusterChannel))
}

func (b *Broadcaster) subscribeLoop(getPool func(pattern string) PenalizerPool) {
	backoff := time.Second
	for {
		select {
		case <-b.ctx.Done():
			return
		default:
		}

		if err := b.subscribe(getPool); err != nil {
			b.logger.Warn("cluster subscriber disconnected, reconnecting",
				zap.Error(err),
				zap.Duration("backoff", backoff),
			)
			select {
			case <-b.ctx.Done():
				return
			case <-time.After(backoff):
			}
			if backoff < 30*time.Second {
				backoff *= 2
			}
		} else {
			backoff = time.Second
		}
	}
}

func (b *Broadcaster) subscribe(getPool func(pattern string) PenalizerPool) error {
	sub := b.rdb.Subscribe(b.ctx, clusterChannel)
	defer sub.Close()

	// Verify subscription
	_, err := sub.Receive(b.ctx)
	if err != nil {
		return fmt.Errorf("subscribe failed: %w", err)
	}

	ch := sub.Channel()
	for {
		select {
		case <-b.ctx.Done():
			return nil
		case msg, ok := <-ch:
			if !ok {
				return fmt.Errorf("subscription channel closed")
			}
			b.handleEvent(msg.Payload, getPool)
		}
	}
}

func (b *Broadcaster) handleEvent(payload string, getPool func(pattern string) PenalizerPool) {
	var event ClusterEvent
	if err := json.Unmarshal([]byte(payload), &event); err != nil {
		b.logger.Debug("failed to decode cluster event", zap.Error(err))
		return
	}

	pool := getPool(event.PoolPattern)
	if pool == nil {
		return
	}

	switch event.Event {
	case EventPenalize:
		pool.PenalizeByCredID(event.CredID, event.UntilNs)
		b.logger.Debug("cluster: applied penalize from peer",
			zap.String("pool", event.PoolPattern),
			zap.Int("cred_id", event.CredID),
		)
	case EventReset:
		pool.ResetByCredID(event.CredID)
		b.logger.Debug("cluster: applied reset from peer",
			zap.String("pool", event.PoolPattern),
			zap.Int("cred_id", event.CredID),
		)
	}
}

// Stop shuts down the broadcaster and subscriber.
func (b *Broadcaster) Stop() {
	if b == nil {
		return
	}
	b.cancel()
}

// PenalizerPool is the interface the cluster subscriber uses to mutate pool state.
// Implemented by BalancedChannelPool via adapter methods.
type PenalizerPool interface {
	PenalizeByCredID(credID int, untilNs int64)
	ResetByCredID(credID int)
}
