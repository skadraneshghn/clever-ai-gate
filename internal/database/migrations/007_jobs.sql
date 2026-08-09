-- Clever AI Gate — Job Scheduling System
-- Migration 007: Jobs, job run history, and scheduler settings

-- ============================================================
-- Jobs: Scheduled job definitions
-- ============================================================
CREATE TABLE IF NOT EXISTS jobs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(255) NOT NULL,
    description TEXT DEFAULT '',
    job_type VARCHAR(100) NOT NULL,                  -- registered executor type
    schedule_type VARCHAR(50) NOT NULL DEFAULT 'cron', -- 'cron' | 'interval' | 'one_time' | 'manual'
    cron_expression VARCHAR(100),                    -- e.g. "0 */1 * * *"
    interval_seconds INT,                            -- for 'interval' type
    run_at TIMESTAMPTZ,                              -- for 'one_time' type
    payload JSONB DEFAULT '{}',                      -- arbitrary job config/args
    timezone VARCHAR(100) DEFAULT 'UTC',
    max_retries INT DEFAULT 3,
    retry_delay_seconds INT DEFAULT 30,
    timeout_seconds INT DEFAULT 300,
    is_enabled BOOLEAN DEFAULT TRUE,
    is_singleton BOOLEAN DEFAULT TRUE,               -- prevent overlapping runs
    tags TEXT[] DEFAULT '{}',
    last_run_at TIMESTAMPTZ,
    next_run_at TIMESTAMPTZ,
    last_run_status VARCHAR(50),                     -- 'success' | 'failed' | 'running'
    run_count BIGINT DEFAULT 0,
    success_count BIGINT DEFAULT 0,
    failure_count BIGINT DEFAULT 0,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_jobs_job_type ON jobs(job_type);
CREATE INDEX IF NOT EXISTS idx_jobs_is_enabled ON jobs(is_enabled);
CREATE INDEX IF NOT EXISTS idx_jobs_next_run_at ON jobs(next_run_at);

-- ============================================================
-- Job Runs: Execution history per job
-- ============================================================
CREATE TABLE IF NOT EXISTS job_runs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    job_id UUID NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    status VARCHAR(50) NOT NULL DEFAULT 'pending',   -- 'pending' | 'running' | 'success' | 'failed' | 'cancelled' | 'timeout'
    triggered_by VARCHAR(50) DEFAULT 'scheduler',    -- 'scheduler' | 'manual' | 'retry'
    attempt INT DEFAULT 1,
    started_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    duration_ms BIGINT DEFAULT 0,
    output TEXT,
    error_message TEXT,
    host VARCHAR(255),                               -- which server instance ran it
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_job_runs_job_id ON job_runs(job_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_job_runs_status ON job_runs(status);
CREATE INDEX IF NOT EXISTS idx_job_runs_created ON job_runs(created_at DESC);

-- ============================================================
-- Scheduler Settings: Professional scheduler configuration
-- ============================================================
CREATE TABLE IF NOT EXISTS scheduler_settings (
    key VARCHAR(100) PRIMARY KEY,
    value TEXT NOT NULL,
    description TEXT DEFAULT '',
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- Seed default settings
INSERT INTO scheduler_settings (key, value, description) VALUES
    ('max_concurrent_jobs',   '10',          'Maximum number of jobs running simultaneously'),
    ('job_timeout',           '300',         'Default job timeout in seconds (0 = no timeout)'),
    ('max_retries',           '3',           'Default maximum retry attempts per job'),
    ('retry_backoff',         'exponential', 'Retry backoff strategy: linear | exponential | fixed'),
    ('retry_delay',           '30',          'Base retry delay in seconds'),
    ('dlq_enabled',           'true',        'Enable dead-letter queue for permanently failed jobs'),
    ('dlq_ttl',               '604800',      'Dead-letter queue TTL in seconds (default: 7 days)'),
    ('timezone',              'UTC',         'Scheduler timezone (e.g. UTC, America/New_York)'),
    ('singleton_mode',        'true',        'Prevent overlapping runs of the same job by default'),
    ('paused',                'false',       'Globally pause all job scheduling'),
    ('log_retention_days',    '30',          'Days to retain job run history records'),
    ('worker_pool_size',      '5',           'Number of async queue worker goroutines'),
    ('queue_key',             'cag:jobs:queue', 'Redis list key for the async job queue'),
    ('dlq_key',               'cag:jobs:dlq',  'Redis list key for the dead-letter queue'),
    ('heartbeat_interval',    '30',          'Scheduler heartbeat interval in seconds')
ON CONFLICT (key) DO NOTHING;

-- ============================================================
-- Auto-update updated_at on jobs
-- ============================================================
CREATE OR REPLACE FUNCTION update_jobs_updated_at() RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_jobs_updated_at ON jobs;
CREATE TRIGGER trg_jobs_updated_at
    BEFORE UPDATE ON jobs
    FOR EACH ROW EXECUTE FUNCTION update_jobs_updated_at();
