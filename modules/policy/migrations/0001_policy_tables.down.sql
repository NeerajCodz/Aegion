-- =============================================================================
-- Policy Module Tables Rollback
-- Migration: 0001_policy_tables
-- =============================================================================

DROP TRIGGER IF EXISTS update_pol_rebac_namespaces_updated_at ON pol_rebac_namespaces;
DROP TRIGGER IF EXISTS update_pol_abac_rules_updated_at ON pol_abac_rules;
DROP TRIGGER IF EXISTS update_pol_roles_updated_at ON pol_roles;

DROP TABLE IF EXISTS pol_rebac_tuples;
DROP TABLE IF EXISTS pol_rebac_namespaces;
DROP TABLE IF EXISTS pol_abac_rules;
DROP TABLE IF EXISTS pol_role_assignments;
DROP TABLE IF EXISTS pol_permissions;
DROP TABLE IF EXISTS pol_roles;
