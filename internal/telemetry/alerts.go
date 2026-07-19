package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/redis/go-redis/v9"
)

// AlertManager monitors key failover activity across the gateway cluster
// and sends webhooks if a threshold is crossed within a sliding window.
type AlertManager struct {
	redis      *redis.Client
	threshold  int64
	window     time.Duration
	cooldown   time.Duration
	webhookURL string
}

// NewAlertManager creates a cluster AlertManager.
func NewAlertManager(r *redis.Client, threshold int64, window, cooldown time.Duration, webhookURL string) *AlertManager {
	return &AlertManager{
		redis:      r,
		threshold:  threshold,
		window:     window,
		cooldown:   cooldown,
		webhookURL: webhookURL,
	}
}

// TrackFailover records a token rotation event in a Redis sorted set sliding window.
func (am *AlertManager) TrackFailover(ctx context.Context, credID int, model, errReason string) error {
	if am == nil || am.redis == nil {
		return nil
	}

	now := time.Now()
	nowUnix := now.Unix()
	clearBefore := now.Add(-am.window).Unix()

	zsetKey := "gate:telemetry:failovers"
	// Unique member prevents deduplication of separate events in the same timestamp millisecond
	member := fmt.Sprintf("%d:%s:%d", credID, model, now.UnixNano())

	pipe := am.redis.TxPipeline()
	pipe.ZAdd(ctx, zsetKey, redis.Z{Score: float64(nowUnix), Member: member})
	pipe.ZRemRangeByScore(ctx, zsetKey, "0", fmt.Sprintf("%d", clearBefore))
	countCmd := pipe.ZCard(ctx, zsetKey)
	pipe.Expire(ctx, zsetKey, am.window*2) // Auto-expire key when idle

	_, err := pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to process telemetry pipeline transaction: %w", err)
	}

	totalFailovers := countCmd.Val()

	if totalFailovers >= am.threshold {
		am.dispatchClusterAlert(ctx, totalFailovers, model, errReason)
	}

	return nil
}

func (am *AlertManager) dispatchClusterAlert(ctx context.Context, currentCount int64, model, lastError string) {
	if am.webhookURL == "" {
		return
	}

	cooldownKey := "gate:telemetry:alert:silence_lock"

	// Lock ensures only one cluster node dispatches an alert during the cooldown window
	set, err := am.redis.SetNX(ctx, cooldownKey, "locked", am.cooldown).Result()
	if err != nil || !set {
		return
	}

	// Dispatch webhook in a separate goroutine so it doesn't block the request path
	go func() {
		payload := map[string]interface{}{
			"text": fmt.Sprintf("🚨 *Clever AI Gate Alert* 🚨\n"+
				"*Metric:* Key Failover Spike Detected Across Cluster\n"+
				"*Total Rotations:* `%d` within the past rolling window\n"+
				"*Active Impact Model:* `%s`\n"+
				"*Last Upstream Reason:* `%s`",
				currentCount, model, lastError),
		}

		body, _ := json.Marshal(payload)
		req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, am.webhookURL, bytes.NewBuffer(body))
		if err != nil {
			return
		}
		req.Header.Set("Content-Type", "application/json")

		client := &http.Client{Timeout: 4 * time.Second}
		resp, err := client.Do(req)
		if err == nil {
			_ = resp.Body.Close()
		}
	}()
}
