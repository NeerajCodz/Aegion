CREATE TABLE IF NOT EXISTS passkey_credentials (
    id          TEXT PRIMARY KEY,
    identity_id TEXT NOT NULL,
    public_key  TEXT NOT NULL,
    sign_count  BIGINT NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_passkey_credentials_identity_id
    ON passkey_credentials (identity_id);

CREATE TABLE IF NOT EXISTS passkey_challenges (
    id          TEXT PRIMARY KEY,
    identity_id TEXT NOT NULL,
    purpose     TEXT NOT NULL,
    expires_at  TIMESTAMPTZ NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_passkey_challenges_identity_id
    ON passkey_challenges (identity_id);
