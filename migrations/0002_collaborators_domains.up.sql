-- M2: collaborators and custom domains.
-- Idempotent (IF NOT EXISTS). Safe to apply on top of an in-flight M1 deployment.

CREATE TABLE IF NOT EXISTS project_collaborators (
    project_id  uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    user_id     uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role        text NOT NULL DEFAULT 'editor',  -- editor | admin
    added_by    uuid REFERENCES users(id),
    added_at    timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (project_id, user_id)
);
CREATE INDEX IF NOT EXISTS idx_collab_user ON project_collaborators(user_id);

-- Domains. The default-subdomain row (kind='subdomain') is implicit from
-- projects.slug + the platform's CP_APP_BASE_DOMAIN and is NOT stored here.
-- Only custom user-provided domains land in this table.
CREATE TABLE IF NOT EXISTS domains (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id  uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    hostname    text UNIQUE NOT NULL,
    kind        text NOT NULL DEFAULT 'custom',  -- 'custom' for now; 'subdomain' reserved
    verified    boolean NOT NULL DEFAULT false,
    tls_secret  text,            -- name of the K8s Secret holding the TLS cert (M3)
    created_at  timestamptz NOT NULL DEFAULT now(),
    verified_at timestamptz
);
CREATE INDEX IF NOT EXISTS idx_domains_project ON domains(project_id);
