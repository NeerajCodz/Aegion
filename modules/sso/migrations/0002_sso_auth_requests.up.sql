CREATE TABLE IF NOT EXISTS sso_auth_requests (
    request_id TEXT PRIMARY KEY,
    connection_slug TEXT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (expires_at > created_at)
);

CREATE INDEX IF NOT EXISTS idx_sso_auth_requests_expiry ON sso_auth_requests (expires_at);
