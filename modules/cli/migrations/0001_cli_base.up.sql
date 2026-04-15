CREATE TABLE IF NOT EXISTS cli_command_runs (
    id TEXT PRIMARY KEY,
    command_name TEXT NOT NULL,
    arguments JSONB NOT NULL DEFAULT '{}'::jsonb,
    result JSONB NOT NULL DEFAULT '{}'::jsonb,
    success BOOLEAN NOT NULL DEFAULT TRUE,
    error_message TEXT NOT NULL DEFAULT '',
    executed_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_cli_command_runs_executed_at ON cli_command_runs (executed_at DESC);
