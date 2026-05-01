CREATE TABLE IF NOT EXISTS proxy_upstreams (
    id UUID PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    url TEXT NOT NULL,
    health_check TEXT NOT NULL DEFAULT '/health',
    timeout TEXT NOT NULL DEFAULT '',
    max_connections INTEGER NOT NULL DEFAULT 0,
    headers JSONB NOT NULL DEFAULT '{}'::jsonb,
    circuit_breaker JSONB NOT NULL DEFAULT '{}'::jsonb,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS proxy_routes (
    id TEXT PRIMARY KEY,
    path TEXT NOT NULL,
    methods JSONB NOT NULL DEFAULT '[]'::jsonb,
    require_auth BOOLEAN NOT NULL DEFAULT FALSE,
    required_aal TEXT NOT NULL DEFAULT '',
    capabilities JSONB NOT NULL DEFAULT '[]'::jsonb,
    rate_limit JSONB NOT NULL DEFAULT '{}'::jsonb,
    target TEXT NOT NULL,
    priority INTEGER NOT NULL DEFAULT 0,
    headers JSONB NOT NULL DEFAULT '{}'::jsonb,
    rewrite JSONB NOT NULL DEFAULT '{}'::jsonb,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    description TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_proxy_routes_target ON proxy_routes (target);
CREATE INDEX IF NOT EXISTS idx_proxy_routes_priority ON proxy_routes (priority DESC, id ASC);
