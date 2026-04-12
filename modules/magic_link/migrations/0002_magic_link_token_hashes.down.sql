DROP INDEX IF EXISTS idx_ml_codes_token_prefix;
DROP INDEX IF EXISTS idx_ml_codes_token_hash;
DROP INDEX IF EXISTS idx_ml_codes_code_hash;

ALTER TABLE ml_codes
    DROP COLUMN IF EXISTS token_prefix,
    DROP COLUMN IF EXISTS token_hash,
    DROP COLUMN IF EXISTS code_hash;
