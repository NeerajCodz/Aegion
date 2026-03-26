-- Rollback: Admin Module Tables

DROP TRIGGER IF EXISTS update_adm_api_keys_updated_at ON adm_api_keys;
DROP TRIGGER IF EXISTS update_adm_operators_updated_at ON adm_operators;
DROP TRIGGER IF EXISTS update_adm_roles_updated_at ON adm_roles;

DROP TABLE IF EXISTS adm_api_keys;
DROP TABLE IF EXISTS adm_audit_log;
DROP TABLE IF EXISTS adm_operators;
DROP TABLE IF EXISTS adm_roles;
