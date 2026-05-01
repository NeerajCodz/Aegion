-- =============================================================================
-- Magic Link Module Tables
-- Migration: 0001_magic_link_codes
-- =============================================================================

-- Magic Link / OTP Codes
CREATE TABLE ml_codes (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    identity_id UUID,  -- NULL for login (lookup by recipient)
    recipient   TEXT NOT NULL,  -- email or phone
    type        TEXT NOT NULL,  -- login, verification, recovery
    code        TEXT NOT NULL,  -- 6-digit OTP code
    token       TEXT NOT NULL UNIQUE,  -- magic link token
    used        BOOLEAN NOT NULL DEFAULT FALSE,
    used_at     TIMESTAMPTZ,
    expires_at  TIMESTAMPTZ NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    
    CONSTRAINT ml_codes_type_check
        CHECK (type IN ('login', 'verification', 'recovery'))
);

CREATE INDEX idx_ml_codes_recipient ON ml_codes(recipient, type, used) 
    WHERE used = FALSE;
CREATE INDEX idx_ml_codes_token ON ml_codes(token) 
    WHERE used = FALSE;
CREATE INDEX idx_ml_codes_expires ON ml_codes(expires_at) 
    WHERE used = FALSE;
CREATE INDEX idx_ml_codes_identity ON ml_codes(identity_id) 
    WHERE identity_id IS NOT NULL;

-- Rate limiting table for magic link requests
CREATE TABLE ml_rate_limits (
    key        TEXT PRIMARY KEY,  -- rate limit key (e.g., "login:email@example.com")
    count      INT NOT NULL DEFAULT 1,
    window_end TIMESTAMPTZ NOT NULL
);

CREATE INDEX idx_ml_rate_limits_window ON ml_rate_limits(window_end);
