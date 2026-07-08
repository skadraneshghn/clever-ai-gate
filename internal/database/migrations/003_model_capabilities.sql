-- Clever AI Gate — Model Capabilities Migration
-- Adds a JSONB capabilities column to model_pools for storing
-- heuristically-detected model feature flags.
--
-- ADD COLUMN with a DEFAULT is a metadata-only operation in PostgreSQL 11+
-- (no table rewrite) — safe to run on a live deployment.

ALTER TABLE model_pools
    ADD COLUMN IF NOT EXISTS capabilities JSONB NOT NULL DEFAULT '{}';

-- Index for efficient capability-based filtering (future use)
CREATE INDEX IF NOT EXISTS idx_model_pools_capabilities
    ON model_pools USING gin(capabilities);
