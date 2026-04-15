DROP TRIGGER IF EXISTS update_adm_scim_mappings_updated_at ON adm_scim_mappings;
DROP TRIGGER IF EXISTS update_adm_scim_groups_updated_at ON adm_scim_groups;

DROP TABLE IF EXISTS adm_scim_tokens;
DROP TABLE IF EXISTS adm_scim_mappings;
DROP TABLE IF EXISTS adm_scim_groups;
