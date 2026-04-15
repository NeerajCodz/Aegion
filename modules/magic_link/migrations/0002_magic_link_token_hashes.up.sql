ALTER TABLE ml_codes
    ADD COLUMN IF NOT EXISTS code_hash TEXT,
    ADD COLUMN IF NOT EXISTS token_hash TEXT,
    ADD COLUMN IF NOT EXISTS token_prefix TEXT;

UPDATE ml_codes
SET code_hash = COALESCE(code_hash, encode(digest(code, 'sha256'), 'base64')),
    token_hash = COALESCE(token_hash, encode(digest(token, 'sha256'), 'base64')),
    token_prefix = COALESCE(token_prefix, LEFT(token, 12))
WHERE code IS NOT NULL
   OR token IS NOT NULL;

ALTER TABLE ml_codes
    ALTER COLUMN code DROP NOT NULL,
    ALTER COLUMN token DROP NOT NULL;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'ml_codes_token_key'
    ) THEN
        ALTER TABLE ml_codes DROP CONSTRAINT ml_codes_token_key;
    END IF;
END $$;

DROP INDEX IF EXISTS idx_ml_codes_token;
CREATE INDEX IF NOT EXISTS idx_ml_codes_code_hash ON ml_codes(recipient, type, code_hash) WHERE used = FALSE AND code_hash IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_ml_codes_token_hash ON ml_codes(token_hash) WHERE token_hash IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_ml_codes_token_prefix ON ml_codes(token_prefix) WHERE used = FALSE AND token_prefix IS NOT NULL;

UPDATE ml_codes
SET code = NULL,
    token = NULL
WHERE code_hash IS NOT NULL;
