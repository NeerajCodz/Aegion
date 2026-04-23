ALTER TABLE proxy_upstreams
ADD COLUMN IF NOT EXISTS health_check_expected_body TEXT NOT NULL DEFAULT '';
