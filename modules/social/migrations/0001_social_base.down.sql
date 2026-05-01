DROP TRIGGER IF EXISTS update_soc_identity_links_updated_at ON soc_identity_links;
DROP TRIGGER IF EXISTS update_soc_provider_credentials_updated_at ON soc_provider_credentials;
DROP TRIGGER IF EXISTS update_soc_providers_updated_at ON soc_providers;

DROP TABLE IF EXISTS soc_identity_links;
DROP TABLE IF EXISTS soc_auth_states;
DROP TABLE IF EXISTS soc_provider_credentials;
DROP TABLE IF EXISTS soc_providers;
