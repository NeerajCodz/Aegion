CREATE TABLE IF NOT EXISTS mfa_enrollments (
    id                TEXT PRIMARY KEY,
    identity_id       TEXT NOT NULL,
    secret_ciphertext TEXT NOT NULL,
    expires_at        TIMESTAMPTZ NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS mfa_totp_factors (
    identity_id       TEXT PRIMARY KEY,
    secret_ciphertext TEXT NOT NULL,
    enrolled_at       TIMESTAMPTZ NOT NULL,
    last_used_at      TIMESTAMPTZ,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS mfa_backup_codes (
    id          TEXT PRIMARY KEY,
    identity_id TEXT NOT NULL,
    code_hash   TEXT NOT NULL,
    batch_id    TEXT NOT NULL,
    used_at     TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_mfa_backup_codes_identity_id
    ON mfa_backup_codes (identity_id);

CREATE TABLE IF NOT EXISTS mfa_trusted_devices (
    id           TEXT PRIMARY KEY,
    identity_id  TEXT NOT NULL,
    token_hash   TEXT NOT NULL,
    token_prefix TEXT NOT NULL,
    label        TEXT,
    expires_at   TIMESTAMPTZ NOT NULL,
    last_used_at TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    revoked_at   TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_mfa_trusted_devices_lookup
    ON mfa_trusted_devices (identity_id, token_hash, token_prefix);
