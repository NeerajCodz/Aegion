-- =============================================================================
-- Admin Module Dynamic Roles rollback
-- Migration: 0002_admin_dynamic_roles
-- =============================================================================

ALTER TABLE adm_operators
    DROP CONSTRAINT IF EXISTS fk_adm_operators_role;

ALTER TABLE adm_operators
    ADD CONSTRAINT adm_operators_role_check
        CHECK (role IN ('super_admin', 'admin', 'operator', 'viewer'));
