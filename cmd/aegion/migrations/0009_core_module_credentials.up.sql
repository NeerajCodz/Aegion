CREATE TABLE core_module_credentials (
    id          TEXT PRIMARY KEY,
    module_id   TEXT NOT NULL UNIQUE,
    secret_hash TEXT NOT NULL,
    permissions TEXT[] NOT NULL,
    audiences   TEXT[] NOT NULL,
    enabled     BOOLEAN NOT NULL DEFAULT TRUE,
    expires_at  TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    rotated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    revoked_at  TIMESTAMPTZ,
    CHECK (array_length(permissions, 1) > 0),
    CHECK (array_length(audiences, 1) > 0)
);

CREATE INDEX core_module_credentials_enabled_idx
    ON core_module_credentials (module_id)
    WHERE enabled = TRUE;
