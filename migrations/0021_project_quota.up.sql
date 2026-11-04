-- Per-project resource overrides. NULL = use the platform default
-- (system_config quota.*), then the built-in default.
ALTER TABLE projects
    ADD COLUMN IF NOT EXISTS cpu_request text,
    ADD COLUMN IF NOT EXISTS mem_request text,
    ADD COLUMN IF NOT EXISTS cpu_limit   text,
    ADD COLUMN IF NOT EXISTS mem_limit   text;
