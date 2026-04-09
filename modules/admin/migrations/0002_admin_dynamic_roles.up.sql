-- =============================================================================
-- Admin Module Dynamic Roles
-- Migration: 0002_admin_dynamic_roles
-- =============================================================================

ALTER TABLE adm_operators
    DROP CONSTRAINT IF EXISTS adm_operators_role_check;

ALTER TABLE adm_operators
    ADD CONSTRAINT fk_adm_operators_role
        FOREIGN KEY (role) REFERENCES adm_roles(name) ON UPDATE CASCADE ON DELETE RESTRICT;
