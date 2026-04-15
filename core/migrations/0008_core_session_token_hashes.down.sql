DROP INDEX IF EXISTS idx_core_sessions_logout_token_hash;
DROP INDEX IF EXISTS idx_core_sessions_token_prefix;
DROP INDEX IF EXISTS idx_core_sessions_token_hash;

ALTER TABLE core_sessions
    DROP COLUMN IF EXISTS logout_token_prefix,
    DROP COLUMN IF EXISTS logout_token_hash,
    DROP COLUMN IF EXISTS token_prefix,
    DROP COLUMN IF EXISTS token_hash;
