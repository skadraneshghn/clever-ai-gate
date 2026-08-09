-- Migration 008: Health Check Sessions & Results
-- Adds persistent storage for exhaustive model pool health check sessions.
-- Each job run creates one session row; each (pool × credential) probe creates one result row.

CREATE TABLE IF NOT EXISTS health_check_sessions (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    trigger_type  VARCHAR(32)  NOT NULL,                     -- 'scheduled', 'manual_full', 'manual_pool'
    status        VARCHAR(32)  NOT NULL DEFAULT 'running',   -- 'running', 'completed', 'failed'
    total_pools   INT          NOT NULL DEFAULT 0,
    total_tasks   INT          NOT NULL DEFAULT 0,
    passed_count  INT          NOT NULL DEFAULT 0,
    failed_count  INT          NOT NULL DEFAULT 0,
    avg_latency_ms FLOAT       NOT NULL DEFAULT 0.0,
    started_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    completed_at  TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS health_check_results (
    id            BIGSERIAL    PRIMARY KEY,
    session_id    UUID         NOT NULL REFERENCES health_check_sessions(id) ON DELETE CASCADE,
    pool_id       BIGINT,
    pool_name     VARCHAR(256) NOT NULL DEFAULT '',
    model_pattern VARCHAR(256) NOT NULL,
    provider_id   VARCHAR(64)  NOT NULL,
    credential_id BIGINT       NOT NULL,
    status_code   INT          NOT NULL,
    is_healthy    BOOLEAN      NOT NULL,
    latency_ms    INT          NOT NULL,
    error_message TEXT         NOT NULL DEFAULT '',
    checked_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_hcr_session_id  ON health_check_results(session_id);
CREATE INDEX IF NOT EXISTS idx_hcr_pool_id     ON health_check_results(pool_id);
CREATE INDEX IF NOT EXISTS idx_hcr_is_healthy  ON health_check_results(is_healthy);
CREATE INDEX IF NOT EXISTS idx_hcs_started_at  ON health_check_sessions(started_at DESC);
