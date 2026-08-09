-- Clever AI Gate — Backfill "huggingface/" prefix for HuggingFace router models
--
-- Providers added via the OpenAI-compatible (custom) auto-discovery flow before
-- the label-as-prefix feature stored models under their RAW name with no prefix.
-- This migration namespaces those models under "huggingface/<model>" and tags the
-- matching credentials with prefix='huggingface' so the routing layer strips it
-- before forwarding upstream.
--
-- The original clean-name pools are preserved as aliases (matching the current
-- discovery behaviour, which registers both forms). Fully idempotent: safe to
-- re-run and a no-op on databases without HuggingFace credentials.

-- 1. Tag HuggingFace router credentials with the "huggingface" routing prefix.
UPDATE credentials
SET prefix = 'huggingface'
WHERE base_url = 'https://router.huggingface.co/v1'
  AND COALESCE(prefix, '') = '';

-- 2. Create namespaced pools ("huggingface/<model>") for every clean pool that
--    has a HuggingFace credential bound to it, copying strategy, fallback and
--    capabilities. Pools that are already namespaced or already exist are skipped.
INSERT INTO model_pools (model_pattern, strategy, fallback_pool_id, capabilities)
SELECT DISTINCT 'huggingface/' || mp.model_pattern, mp.strategy, mp.fallback_pool_id, mp.capabilities
FROM model_pools mp
JOIN credentials c ON c.pool_id = mp.id
WHERE c.base_url = 'https://router.huggingface.co/v1'
  AND c.prefix = 'huggingface'
  AND mp.model_pattern NOT LIKE 'huggingface/%'
ON CONFLICT (model_pattern) DO NOTHING;

-- 3. Bind the HuggingFace credentials to the newly created namespaced pools.
--    A NOT EXISTS guard prevents duplicate bindings on re-runs (the credentials
--    table has no unique constraint on (pool_id, encrypted_key)).
INSERT INTO credentials (pool_id, provider, encrypted_key, base_url, weight, is_healthy, prefix)
SELECT ns.id, c.provider, c.encrypted_key, c.base_url, c.weight, c.is_healthy, c.prefix
FROM credentials c
JOIN model_pools clean ON clean.id = c.pool_id
JOIN model_pools ns ON ns.model_pattern = 'huggingface/' || clean.model_pattern
WHERE c.base_url = 'https://router.huggingface.co/v1'
  AND c.prefix = 'huggingface'
  AND clean.model_pattern NOT LIKE 'huggingface/%'
  AND NOT EXISTS (
    SELECT 1 FROM credentials existing
    WHERE existing.pool_id = ns.id
      AND existing.encrypted_key = c.encrypted_key
  );
