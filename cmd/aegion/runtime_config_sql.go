package main

const (
	sqlSelectSystemConfigByKey = `
		SELECT value
		FROM core_system_config
		WHERE key = $1
	`

	sqlUpsertSystemConfig = `
		INSERT INTO core_system_config (key, value, updated_at)
		VALUES ($1, $2::jsonb, NOW())
		ON CONFLICT (key)
		DO UPDATE SET value = EXCLUDED.value, updated_at = NOW()
	`
)
