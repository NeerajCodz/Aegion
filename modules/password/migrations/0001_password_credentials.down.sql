-- Rollback: Password Module Tables

DROP TRIGGER IF EXISTS update_pwd_credentials_updated_at ON pwd_credentials;
DROP TABLE IF EXISTS pwd_history;
DROP TABLE IF EXISTS pwd_credentials;
