// Package events provides async event publishing helpers for admin operations.
// All publish methods are fire-and-forget goroutines — they never block the
// HTTP handler returning a response to the admin client.
package events

import (
	"context"
	"encoding/json"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

const (
	adminEventsChannel = "clever_gate:admin_events"
)

// AdminEventType describes an admin mutation operation.
type AdminEventType string

const (
	EventTenantCreated   AdminEventType = "tenant.created"
	EventTenantUpdated   AdminEventType = "tenant.updated"
	EventTenantDeleted   AdminEventType = "tenant.deleted"
	EventPoolUpdated     AdminEventType = "pool.updated"
	EventCredUpdated     AdminEventType = "credential.updated"
)

// AdminEvent is the event payload published when an admin mutates data.
type AdminEvent struct {
	Type      AdminEventType `json:"type"`
	EntityID  string         `json:"entity_id,omitempty"`
	APIKey    string         `json:"api_key,omitempty"` // For tenant invalidation
	Timestamp int64          `json:"ts"`
}

// Publisher publishes admin events to Redis for cross-node cache invalidation.
// When rdb is nil, all operations are no-ops.
type Publisher struct {
	rdb    *redis.Client
	logger *zap.Logger
}

// NewPublisher creates a Publisher. Returns a no-op if rdb is nil.
func NewPublisher(rdb *redis.Client, logger *zap.Logger) *Publisher {
	return &Publisher{rdb: rdb, logger: logger}
}

// PublishTenantChange fires an async Redis PUBLISH for a tenant mutation.
// Peer nodes will invalidate their tenant cache entry on receipt.
func (p *Publisher) PublishTenantChange(eventType AdminEventType, tenantID, apiKey string) {
	if p == nil || p.rdb == nil {
		return
	}
	go p.publish(AdminEvent{
		Type:      eventType,
		EntityID:  tenantID,
		APIKey:    apiKey,
		Timestamp: time.Now().UnixMilli(),
	})
}

// PublishPoolChange fires an async Redis PUBLISH for a pool/credential mutation.
func (p *Publisher) PublishPoolChange(eventType AdminEventType, poolID string) {
	if p == nil || p.rdb == nil {
		return
	}
	go p.publish(AdminEvent{
		Type:      eventType,
		EntityID:  poolID,
		Timestamp: time.Now().UnixMilli(),
	})
}

func (p *Publisher) publish(event AdminEvent) {
	data, err := json.Marshal(event)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := p.rdb.Publish(ctx, adminEventsChannel, data).Err(); err != nil {
		p.logger.Debug("admin event publish failed (non-critical)", zap.Error(err))
	}
}
