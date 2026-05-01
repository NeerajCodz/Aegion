ALTER TABLE core_sessions
    ADD COLUMN IF NOT EXISTS token_hash TEXT,
    ADD COLUMN IF NOT EXISTS token_prefix TEXT,
    ADD COLUMN IF NOT EXISTS logout_token_hash TEXT,
    ADD COLUMN IF NOT EXISTS logout_token_prefix TEXT;

UPDATE core_sessions
SET token_hash = COALESCE(token_hash, encode(digest(token, 'sha256'), 'base64')),
    token_prefix = COALESCE(token_prefix, LEFT(token, 12)),
    logout_token_hash = CASE
        WHEN logout_token IS NULL THEN logout_token_hash
        ELSE COALESCE(logout_token_hash, encode(digest(logout_token, 'sha256'), 'base64'))
    END,
    logout_token_prefix = CASE
        WHEN logout_token IS NULL THEN logout_token_prefix
        ELSE COALESCE(logout_token_prefix, LEFT(logout_token, 12))
    END
WHERE token IS NOT NULL
   OR logout_token IS NOT NULL;

ALTER TABLE core_sessions
    ALTER COLUMN token DROP NOT NULL;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'core_sessions_token_key'
    ) THEN
        ALTER TABLE core_sessions DROP CONSTRAINT core_sessions_token_key;
    END IF;
    IF EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'core_sessions_logout_token_key'
    ) THEN
        ALTER TABLE core_sessions DROP CONSTRAINT core_sessions_logout_token_key;
    END IF;
END $$;

DROP INDEX IF EXISTS idx_core_sessions_token;
CREATE UNIQUE INDEX IF NOT EXISTS idx_core_sessions_token_hash ON core_sessions(token_hash) WHERE token_hash IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_core_sessions_token_prefix ON core_sessions(token_prefix) WHERE active = TRUE AND token_prefix IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_core_sessions_logout_token_hash ON core_sessions(logout_token_hash) WHERE logout_token_hash IS NOT NULL;

UPDATE core_sessions
SET token = NULL,
    logout_token = NULL
WHERE token_hash IS NOT NULL;
