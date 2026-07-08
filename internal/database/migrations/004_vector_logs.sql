-- Clever AI Gate — Vector Logs Migration
-- Configures pgvector if available, and provisions request_vector_logs.
-- Fallback structure is built using standard float8[] arrays if pgvector is absent.

DO $$
DECLARE
    vector_available BOOLEAN;
BEGIN
    SELECT EXISTS (
        SELECT 1 FROM pg_available_extensions WHERE name = 'vector'
    ) INTO vector_available;

    IF vector_available THEN
        EXECUTE 'CREATE EXTENSION IF NOT EXISTS vector';
        
        CREATE TABLE IF NOT EXISTS request_vector_logs (
            id BIGSERIAL PRIMARY KEY,
            log_id BIGINT UNIQUE NOT NULL REFERENCES request_logs(id) ON DELETE CASCADE,
            prompt_text TEXT,
            response_text TEXT,
            prompt_embedding vector(1536),
            created_at TIMESTAMPTZ DEFAULT NOW()
        );
        
        CREATE INDEX IF NOT EXISTS idx_request_vector_logs_embedding 
            ON request_vector_logs USING hnsw (prompt_embedding vector_cosine_ops);
    ELSE
        -- Fallback table if pgvector is not installed on the system database
        CREATE TABLE IF NOT EXISTS request_vector_logs (
            id BIGSERIAL PRIMARY KEY,
            log_id BIGINT UNIQUE NOT NULL REFERENCES request_logs(id) ON DELETE CASCADE,
            prompt_text TEXT,
            response_text TEXT,
            prompt_embedding float8[], -- standard float array fallback
            created_at TIMESTAMPTZ DEFAULT NOW()
        );
    END IF;
END $$;
