-- Clever AI Gate — Initial Schema
-- This migration creates the core tables for the routing engine.

-- ============================================================
-- Tenants: Organizations/users with virtual API keys
-- ============================================================
CREATE TABLE IF NOT EXISTS tenants (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(255) NOT NULL,
    api_key VARCHAR(64) NOT NULL UNIQUE,
    token_balance BIGINT NOT NULL DEFAULT 1000000000,
    is_active BOOLEAN DEFAULT TRUE,
    rate_limit_rpm INT DEFAULT 60,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_tenants_api_key ON tenants(api_key);

-- ============================================================
-- Model Routing Pools: Maps model patterns to credential groups
-- ============================================================
CREATE TABLE IF NOT EXISTS model_pools (
    id SERIAL PRIMARY KEY,
    model_pattern VARCHAR(150) NOT NULL UNIQUE,
    strategy VARCHAR(50) DEFAULT 'round-robin',
    fallback_pool_id INT REFERENCES model_pools(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

-- ============================================================
-- Credentials: Provider API keys (encrypted at rest)
-- ============================================================
CREATE TABLE IF NOT EXISTS credentials (
    id SERIAL PRIMARY KEY,
    pool_id INT NOT NULL REFERENCES model_pools(id) ON DELETE CASCADE,
    provider VARCHAR(50) NOT NULL,
    encrypted_key TEXT NOT NULL,
    base_url TEXT NOT NULL,
    weight INT DEFAULT 1,
    is_healthy BOOLEAN DEFAULT TRUE,
    last_error TEXT,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_credentials_pool ON credentials(pool_id);

-- ============================================================
-- Request Logs: Append-only telemetry (async bulk insert target)
-- ============================================================
CREATE TABLE IF NOT EXISTS request_logs (
    id BIGSERIAL PRIMARY KEY,
    tenant_id UUID REFERENCES tenants(id) ON DELETE SET NULL,
    model VARCHAR(150),
    provider VARCHAR(50),
    prompt_tokens INT DEFAULT 0,
    completion_tokens INT DEFAULT 0,
    latency_ms INT DEFAULT 0,
    status_code INT DEFAULT 0,
    error_message TEXT,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_request_logs_tenant ON request_logs(tenant_id, created_at);
CREATE INDEX IF NOT EXISTS idx_request_logs_created ON request_logs(created_at);

-- ============================================================
-- NOTIFY triggers: Push config changes to running gateway instances
-- ============================================================
CREATE OR REPLACE FUNCTION notify_config_change() RETURNS TRIGGER AS $$
BEGIN
    PERFORM pg_notify('config_change', TG_TABLE_NAME || ':' || COALESCE(NEW.id::text, OLD.id::text));
    RETURN COALESCE(NEW, OLD);
END;
$$ LANGUAGE plpgsql;

-- Trigger on model_pools changes
DROP TRIGGER IF EXISTS trg_model_pools_change ON model_pools;
CREATE TRIGGER trg_model_pools_change
    AFTER INSERT OR UPDATE OR DELETE ON model_pools
    FOR EACH ROW EXECUTE FUNCTION notify_config_change();

-- Trigger on credentials changes
DROP TRIGGER IF EXISTS trg_credentials_change ON credentials;
CREATE TRIGGER trg_credentials_change
    AFTER INSERT OR UPDATE OR DELETE ON credentials
    FOR EACH ROW EXECUTE FUNCTION notify_config_change();

-- Trigger on tenants changes
DROP TRIGGER IF EXISTS trg_tenants_change ON tenants;
CREATE TRIGGER trg_tenants_change
    AFTER INSERT OR UPDATE OR DELETE ON tenants
    FOR EACH ROW EXECUTE FUNCTION notify_config_change();
