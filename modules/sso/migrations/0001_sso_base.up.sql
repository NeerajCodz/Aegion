CREATE TABLE IF NOT EXISTS sso_connections (
    id UUID PRIMARY KEY,
    slug TEXT NOT NULL UNIQUE,
    display_name TEXT NOT NULL,
    entity_id TEXT NOT NULL,
    sso_url TEXT NOT NULL,
    certificate_pem TEXT NOT NULL DEFAULT '',
    metadata_url TEXT NOT NULL DEFAULT '',
    domains JSONB NOT NULL DEFAULT '[]'::jsonb,
    attribute_mapping JSONB NOT NULL DEFAULT '{}'::jsonb,
    jit_provisioning BOOLEAN NOT NULL DEFAULT TRUE,
    default_redirect_to TEXT NOT NULL DEFAULT '/',
    extra_authn_context JSONB NOT NULL DEFAULT '{}'::jsonb,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_sso_connections_slug ON sso_connections (slug);
