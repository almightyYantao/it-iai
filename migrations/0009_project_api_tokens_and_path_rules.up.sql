-- Project-level API token + per-path auth rules.
--
-- The token is a single shared secret per project, stored bcrypt-hashed (we
-- never need the plaintext after issue: the verify path bcrypt-compares the
-- presented Bearer against the hash). The 8-char prefix is the only thing the
-- UI shows back — enough for owners to identify which token a teammate
-- pasted somewhere, not enough to forge a request.
ALTER TABLE projects
    ADD COLUMN IF NOT EXISTS api_token_hash       bytea,
    ADD COLUMN IF NOT EXISTS api_token_prefix     text,
    ADD COLUMN IF NOT EXISTS api_token_created_at timestamptz;

-- Per-path-prefix auth override. Each row says "for requests whose URL path
-- starts with <path_prefix>, replace the project's default SSO gate with
-- <mode>". Modes:
--   no_auth  — skip ForwardAuth entirely (still subject to the IP allow-list).
--   token    — require Authorization: Bearer <project api token>.
--
-- Longer prefixes win when multiple rules could match; the deployer sorts and
-- emits the IngressRoute routes in that order so traefik's router-priority
-- handles selection correctly.
CREATE TABLE IF NOT EXISTS project_path_rules (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id  uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    path_prefix text NOT NULL,
    mode        text NOT NULL CHECK (mode IN ('no_auth', 'token')),
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (project_id, path_prefix)
);

CREATE INDEX IF NOT EXISTS project_path_rules_project_idx
    ON project_path_rules(project_id);
