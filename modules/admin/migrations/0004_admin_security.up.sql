CREATE TABLE IF NOT EXISTS adm_ip_bans (
    id UUID PRIMARY KEY,
    cidr TEXT NOT NULL UNIQUE,
    reason TEXT NOT NULL,
    created_by UUID NULL REFERENCES adm_operators(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ NULL
);

CREATE INDEX IF NOT EXISTS idx_adm_ip_bans_cidr ON adm_ip_bans (cidr);
CREATE INDEX IF NOT EXISTS idx_adm_ip_bans_expires_at ON adm_ip_bans (expires_at);
