package jobs

import (
	"context"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

// SettingsStore loads and persists SchedulerSettings from/to PostgreSQL.
type SettingsStore struct {
	db     *pgxpool.Pool
	logger *zap.Logger
}

// NewSettingsStore creates a new settings store.
func NewSettingsStore(db *pgxpool.Pool, logger *zap.Logger) *SettingsStore {
	return &SettingsStore{db: db, logger: logger}
}

// Load reads all settings from the database and returns a populated SchedulerSettings.
// Falls back to defaults for any missing keys.
func (s *SettingsStore) Load(ctx context.Context) (SchedulerSettings, error) {
	cfg := DefaultSettings()

	rows, err := s.db.Query(ctx, `SELECT key, value FROM scheduler_settings`)
	if err != nil {
		return cfg, fmt.Errorf("failed to query scheduler_settings: %w", err)
	}
	defer rows.Close()

	kv := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return cfg, fmt.Errorf("failed to scan setting row: %w", err)
		}
		kv[k] = v
	}

	applySettings(&cfg, kv)
	return cfg, nil
}

// Save persists a single setting key/value to the database (UPSERT).
func (s *SettingsStore) Save(ctx context.Context, key, value string) error {
	_, err := s.db.Exec(ctx, `
		INSERT INTO scheduler_settings (key, value, updated_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = NOW()
	`, key, value)
	if err != nil {
		return fmt.Errorf("failed to save setting %q: %w", key, err)
	}
	return nil
}

// SaveAll persists a full SchedulerSettings struct to the database.
func (s *SettingsStore) SaveAll(ctx context.Context, cfg SchedulerSettings) error {
	pairs := settingsToMap(cfg)
	for k, v := range pairs {
		if err := s.Save(ctx, k, v); err != nil {
			return err
		}
	}
	s.logger.Info("scheduler settings saved", zap.Int("count", len(pairs)))
	return nil
}

// GetAll returns all raw key/value settings from the database.
func (s *SettingsStore) GetAll(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.Query(ctx, `SELECT key, value, description, updated_at::text FROM scheduler_settings ORDER BY key`)
	if err != nil {
		return nil, fmt.Errorf("failed to query scheduler_settings: %w", err)
	}
	defer rows.Close()

	result := make(map[string]string)
	for rows.Next() {
		var k, v, desc, updatedAt string
		if err := rows.Scan(&k, &v, &desc, &updatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan setting: %w", err)
		}
		result[k] = v
	}
	return result, nil
}

// GetAllWithMeta returns settings with description and updated_at metadata.
func (s *SettingsStore) GetAllWithMeta(ctx context.Context) ([]SettingRow, error) {
	rows, err := s.db.Query(ctx, `SELECT key, value, description, updated_at::text FROM scheduler_settings ORDER BY key`)
	if err != nil {
		return nil, fmt.Errorf("failed to query scheduler_settings: %w", err)
	}
	defer rows.Close()

	var result []SettingRow
	for rows.Next() {
		var r SettingRow
		if err := rows.Scan(&r.Key, &r.Value, &r.Description, &r.UpdatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan setting: %w", err)
		}
		result = append(result, r)
	}
	return result, nil
}

// SettingRow represents a single setting row with metadata.
type SettingRow struct {
	Key         string `json:"key"`
	Value       string `json:"value"`
	Description string `json:"description"`
	UpdatedAt   string `json:"updated_at"`
}

// applySettings maps raw key/value pairs onto a SchedulerSettings struct.
func applySettings(cfg *SchedulerSettings, kv map[string]string) {
	if v, ok := kv["max_concurrent_jobs"]; ok {
		cfg.MaxConcurrentJobs = parseInt(v, cfg.MaxConcurrentJobs)
	}
	if v, ok := kv["job_timeout"]; ok {
		cfg.JobTimeout = parseInt(v, cfg.JobTimeout)
	}
	if v, ok := kv["max_retries"]; ok {
		cfg.MaxRetries = parseInt(v, cfg.MaxRetries)
	}
	if v, ok := kv["retry_backoff"]; ok {
		cfg.RetryBackoff = RetryBackoff(v)
	}
	if v, ok := kv["retry_delay"]; ok {
		cfg.RetryDelay = parseInt(v, cfg.RetryDelay)
	}
	if v, ok := kv["dlq_enabled"]; ok {
		cfg.DLQEnabled = v == "true"
	}
	if v, ok := kv["dlq_ttl"]; ok {
		cfg.DLQTTL = parseInt(v, cfg.DLQTTL)
	}
	if v, ok := kv["timezone"]; ok && v != "" {
		cfg.Timezone = v
	}
	if v, ok := kv["singleton_mode"]; ok {
		cfg.SingletonMode = v == "true"
	}
	if v, ok := kv["paused"]; ok {
		cfg.Paused = v == "true"
	}
	if v, ok := kv["log_retention_days"]; ok {
		cfg.LogRetentionDays = parseInt(v, cfg.LogRetentionDays)
	}
	if v, ok := kv["worker_pool_size"]; ok {
		cfg.WorkerPoolSize = parseInt(v, cfg.WorkerPoolSize)
	}
	if v, ok := kv["queue_key"]; ok && v != "" {
		cfg.QueueKey = v
	}
	if v, ok := kv["dlq_key"]; ok && v != "" {
		cfg.DLQKey = v
	}
	if v, ok := kv["heartbeat_interval"]; ok {
		cfg.HeartbeatInterval = parseInt(v, cfg.HeartbeatInterval)
	}
}

// settingsToMap converts a SchedulerSettings struct to a key/value map for persistence.
func settingsToMap(cfg SchedulerSettings) map[string]string {
	return map[string]string{
		"max_concurrent_jobs":   strconv.Itoa(cfg.MaxConcurrentJobs),
		"job_timeout":           strconv.Itoa(cfg.JobTimeout),
		"max_retries":           strconv.Itoa(cfg.MaxRetries),
		"retry_backoff":         string(cfg.RetryBackoff),
		"retry_delay":           strconv.Itoa(cfg.RetryDelay),
		"dlq_enabled":           boolStr(cfg.DLQEnabled),
		"dlq_ttl":               strconv.Itoa(cfg.DLQTTL),
		"timezone":              cfg.Timezone,
		"singleton_mode":        boolStr(cfg.SingletonMode),
		"paused":                boolStr(cfg.Paused),
		"log_retention_days":    strconv.Itoa(cfg.LogRetentionDays),
		"worker_pool_size":      strconv.Itoa(cfg.WorkerPoolSize),
		"queue_key":             cfg.QueueKey,
		"dlq_key":               cfg.DLQKey,
		"heartbeat_interval":    strconv.Itoa(cfg.HeartbeatInterval),
	}
}

func parseInt(s string, fallback int) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return fallback
	}
	return n
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
