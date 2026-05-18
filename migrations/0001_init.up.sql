-- M1 schema. Trimmed from §5 of the requirements doc to the tables actually used in M1.
-- Later milestones add: project_collaborators, project_acl_entries, domains, audit_logs (full).

CREATE EXTENSION IF NOT EXISTS citext;
CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE IF NOT EXISTS users (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    kc_sub        text UNIQUE,
    email         citext UNIQUE NOT NULL,
    name          text,
    is_admin      boolean NOT NULL DEFAULT false,
    created_at    timestamptz NOT NULL DEFAULT now(),
    last_seen_at  timestamptz
);

CREATE TABLE IF NOT EXISTS projects (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    slug            text UNIQUE NOT NULL,
    name            text NOT NULL,
    owner_id        uuid NOT NULL REFERENCES users(id),
    visibility      text NOT NULL DEFAULT 'org',
    status          text NOT NULL DEFAULT 'created',
    manifest        jsonb,
    created_at      timestamptz NOT NULL DEFAULT now(),
    last_pushed_at  timestamptz,
    last_active_at  timestamptz,
    deleted_at      timestamptz
);
CREATE INDEX IF NOT EXISTS idx_projects_owner ON projects(owner_id);

CREATE TABLE IF NOT EXISTS deployments (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id        uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    trigger_type      text NOT NULL,
    triggered_by      text NOT NULL,
    status            text NOT NULL DEFAULT 'pending',
    source_blob_key   text,
    image_tag         text,
    manifest          jsonb NOT NULL,
    failure_reason    text,
    created_at        timestamptz NOT NULL DEFAULT now(),
    queued_at         timestamptz,
    build_started_at  timestamptz,
    build_done_at     timestamptz,
    deployed_at       timestamptz
);
CREATE INDEX IF NOT EXISTS idx_deploy_proj_created ON deployments(project_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_deploy_active_status ON deployments(status)
  WHERE status IN ('queued','building','pushing','deploying');

CREATE TABLE IF NOT EXISTS deployment_events (
    id            bigserial PRIMARY KEY,
    deployment_id uuid NOT NULL REFERENCES deployments(id) ON DELETE CASCADE,
    phase         text NOT NULL,        -- upload | build | push | deploy | runtime
    level         text NOT NULL,        -- info | warn | error
    message       text NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_deploy_events_dep ON deployment_events(deployment_id, id);

CREATE TABLE IF NOT EXISTS project_env (
    project_id      uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    key             text NOT NULL,
    value_encrypted bytea NOT NULL,
    updated_at      timestamptz NOT NULL DEFAULT now(),
    updated_by      uuid REFERENCES users(id),
    PRIMARY KEY (project_id, key)
);

CREATE TABLE IF NOT EXISTS deploy_tokens (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id    uuid REFERENCES projects(id) ON DELETE CASCADE,
                  -- NULL = platform-wide bootstrap token (dev only)
    name          text NOT NULL,
    token_hash    bytea NOT NULL,
    token_prefix  text NOT NULL,
    scopes        text[] NOT NULL,
    allow_db      boolean NOT NULL DEFAULT false,
    created_by    uuid REFERENCES users(id),
    created_at    timestamptz NOT NULL DEFAULT now(),
    expires_at    timestamptz NOT NULL,
    last_used_at  timestamptz,
    revoked_at    timestamptz
);
CREATE UNIQUE INDEX IF NOT EXISTS deploy_tokens_hash_active
  ON deploy_tokens(token_hash) WHERE revoked_at IS NULL;

CREATE TABLE IF NOT EXISTS audit_logs (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    actor_type  text NOT NULL,
    actor_id    text NOT NULL,
    action      text NOT NULL,
    project_id  uuid,
    metadata    jsonb,
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_audit_proj ON audit_logs(project_id, created_at DESC);
