-- =============================================================================
-- Aegion Database Initialization Script
-- =============================================================================
-- This script runs automatically when the PostgreSQL container starts
-- for the first time. It sets up the database, user, and required extensions.
-- =============================================================================

-- Enable required extensions
-- uuid-ossp: For UUID generation (gen_random_uuid is preferred in PG13+)
-- pgcrypto: For encryption functions (crypt, gen_salt, etc.)
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- Grant all privileges to the aegion user on the aegion database
-- (The user and database are created automatically via POSTGRES_USER/POSTGRES_DB env vars)
GRANT ALL PRIVILEGES ON DATABASE aegion TO aegion;

-- Ensure the aegion user can create schemas and tables
ALTER USER aegion CREATEDB;

-- Create application schema if not exists
CREATE SCHEMA IF NOT EXISTS aegion;

-- Grant schema permissions
GRANT ALL PRIVILEGES ON SCHEMA aegion TO aegion;
GRANT ALL PRIVILEGES ON SCHEMA public TO aegion;

-- Set default search path
ALTER DATABASE aegion SET search_path TO public, aegion;

-- Create a read-only user for analytics/reporting (optional, commented out)
-- Uncomment and configure if you need a read-only database user
-- CREATE USER aegion_readonly WITH PASSWORD 'readonly_password';
-- GRANT CONNECT ON DATABASE aegion TO aegion_readonly;
-- GRANT USAGE ON SCHEMA public TO aegion_readonly;
-- GRANT USAGE ON SCHEMA aegion TO aegion_readonly;
-- GRANT SELECT ON ALL TABLES IN SCHEMA public TO aegion_readonly;
-- GRANT SELECT ON ALL TABLES IN SCHEMA aegion TO aegion_readonly;
-- ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT ON TABLES TO aegion_readonly;
-- ALTER DEFAULT PRIVILEGES IN SCHEMA aegion GRANT SELECT ON TABLES TO aegion_readonly;

-- Log successful initialization
DO $$
BEGIN
    RAISE NOTICE '✓ Aegion database initialized successfully';
    RAISE NOTICE '  - Extensions: uuid-ossp, pgcrypto';
    RAISE NOTICE '  - Schema: aegion';
    RAISE NOTICE '  - User: aegion (full privileges)';
END $$;
