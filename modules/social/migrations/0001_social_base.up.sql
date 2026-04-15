CREATE TABLE IF NOT EXISTS soc_providers (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    slug                 TEXT NOT NULL UNIQUE,
    display_name         TEXT NOT NULL,
    preset               TEXT NOT NULL DEFAULT 'custom',
    protocol             TEXT NOT NULL,
    issuer               TEXT,
    discovery_url        TEXT,
    authorize_endpoint   TEXT,
    token_endpoint       TEXT,
    userinfo_endpoint    TEXT,
    jwks_uri             TEXT,
    scopes               JSONB NOT NULL DEFAULT '[]'::jsonb,
    claim_mapping        JSONB NOT NULL DEFAULT '{}'::jsonb,
    extra_auth_params    JSONB NOT NULL DEFAULT '{}'::jsonb,
    pkce_method          TEXT NOT NULL DEFAULT 'S256',
    auth_style           TEXT NOT NULL DEFAULT 'client_secret_post',
    claim_source         TEXT NOT NULL DEFAULT 'userinfo',
    enabled              BOOLEAN NOT NULL DEFAULT TRUE,
    trust_email_verified BOOLEAN NOT NULL DEFAULT FALSE,
    redirect_uri         TEXT NOT NULL,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT soc_providers_protocol_check
        CHECK (protocol IN ('oidc', 'oauth2')),
    CONSTRAINT soc_providers_pkce_method_check
        CHECK (pkce_method IN ('none', 'plain', 'S256')),
    CONSTRAINT soc_providers_auth_style_check
        CHECK (auth_style IN ('client_secret_post', 'client_secret_basic')),
    CONSTRAINT soc_providers_claim_source_check
        CHECK (claim_source IN ('userinfo', 'id_token', 'github_user'))
);

CREATE INDEX IF NOT EXISTS idx_soc_providers_enabled ON soc_providers(enabled);

CREATE TABLE IF NOT EXISTS soc_provider_credentials (
    provider_id               UUID PRIMARY KEY REFERENCES soc_providers(id) ON DELETE CASCADE,
    client_id                 TEXT NOT NULL,
    client_secret_ciphertext  TEXT NOT NULL DEFAULT '',
    created_at                TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at                TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS soc_auth_states (
    id                       TEXT PRIMARY KEY,
    provider_slug            TEXT NOT NULL,
    redirect_to              TEXT,
    nonce                    TEXT,
    pkce_verifier_ciphertext TEXT,
    expires_at               TIMESTAMPTZ NOT NULL,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_soc_auth_states_provider ON soc_auth_states(provider_slug);
CREATE INDEX IF NOT EXISTS idx_soc_auth_states_expires_at ON soc_auth_states(expires_at);

CREATE TABLE IF NOT EXISTS soc_identity_links (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    provider_slug    TEXT NOT NULL,
    identity_id      UUID NOT NULL REFERENCES core_identities(id) ON DELETE CASCADE,
    provider_subject TEXT NOT NULL,
    email            TEXT,
    email_verified   BOOLEAN NOT NULL DEFAULT FALSE,
    display_name     TEXT,
    picture_url      TEXT,
    raw_claims       JSONB NOT NULL DEFAULT '{}'::jsonb,
    linked_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_login_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT soc_identity_links_provider_subject_unique
        UNIQUE (provider_slug, provider_subject)
);

CREATE INDEX IF NOT EXISTS idx_soc_identity_links_identity_id ON soc_identity_links(identity_id);
CREATE INDEX IF NOT EXISTS idx_soc_identity_links_email ON soc_identity_links(email);

CREATE TRIGGER update_soc_providers_updated_at
    BEFORE UPDATE ON soc_providers
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_soc_provider_credentials_updated_at
    BEFORE UPDATE ON soc_provider_credentials
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_soc_identity_links_updated_at
    BEFORE UPDATE ON soc_identity_links
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
