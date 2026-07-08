-- Clever AI Gate — Add optional routing prefix to credentials
ALTER TABLE credentials ADD COLUMN IF NOT EXISTS prefix VARCHAR(100) DEFAULT '';
