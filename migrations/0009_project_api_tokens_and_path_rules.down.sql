DROP INDEX IF EXISTS project_path_rules_project_idx;
DROP TABLE IF EXISTS project_path_rules;
ALTER TABLE projects
    DROP COLUMN IF EXISTS api_token_created_at,
    DROP COLUMN IF EXISTS api_token_prefix,
    DROP COLUMN IF EXISTS api_token_hash;
