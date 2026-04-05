-- =============================================================================
-- OAuth2 Module Tables - Part 1: Clients
-- Migration: 0001_oauth2_clients
-- =============================================================================

-- OAuth2 Clients
CREATE TABLE oa2_clients (
    id                          TEXT PRIMARY KEY,
    secret_hash                 TEXT,                    -- bcrypt hash (NULL for public clients)
    name                        TEXT NOT NULL,
    description                 TEXT,
    logo_uri                    TEXT,
    client_uri                  TEXT,
    policy_uri                  TEXT,
    tos_uri                     TEXT,
    redirect_uris               TEXT[] NOT NULL DEFAULT '{}',
    post_logout_redirect_uris   TEXT[] DEFAULT '{}',
    grant_types                 TEXT[] NOT NULL DEFAULT '{authorization_code}',
    response_types              TEXT[] NOT NULL DEFAULT '{code}',
    scopes                      TEXT[] NOT NULL DEFAULT '{openid}',
    audience                    TEXT[] DEFAULT '{}',
    token_endpoint_auth_method  TEXT NOT NULL DEFAULT 'client_secret_basic',
    jwks_uri                    TEXT,                    -- for private_key_jwt
    jwks                        JSONB,                   -- inline JWKS
    sector_identifier_uri       TEXT,
    subject_type                TEXT DEFAULT 'public',   -- public or pairwise
    id_token_signed_response_alg TEXT DEFAULT 'RS256',
    access_token_strategy       TEXT DEFAULT 'jwt',      -- jwt or opaque
    access_token_ttl            INTEGER DEFAULT 900,     -- 15 minutes
    refresh_token_ttl           INTEGER DEFAULT 2592000, -- 30 days
    id_token_ttl                INTEGER DEFAULT 3600,    -- 1 hour
    auth_code_ttl               INTEGER DEFAULT 600,     -- 10 minutes
    require_pkce                BOOLEAN NOT NULL DEFAULT true,
    require_consent             BOOLEAN NOT NULL DEFAULT true,
    allow_offline_access        BOOLEAN NOT NULL DEFAULT true,
    metadata                    JSONB DEFAULT '{}',
    owner_id                    TEXT,                    -- admin identity who created this
    created_at                  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at                  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Client name should be unique for display
CREATE UNIQUE INDEX idx_oa2_clients_name ON oa2_clients(name);

-- Lookup by owner for admin management
CREATE INDEX idx_oa2_clients_owner ON oa2_clients(owner_id);

-- Trigger for updated_at
CREATE TRIGGER update_oa2_clients_updated_at
    BEFORE UPDATE ON oa2_clients
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- Client allowed origins (for CORS)
CREATE TABLE oa2_client_origins (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    client_id   TEXT NOT NULL REFERENCES oa2_clients(id) ON DELETE CASCADE,
    origin      TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX idx_oa2_client_origins_unique ON oa2_client_origins(client_id, origin);
CREATE INDEX idx_oa2_client_origins_client ON oa2_client_origins(client_id);
